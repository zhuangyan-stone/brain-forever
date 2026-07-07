// Package cache provides Redis-backed caching for temporary data
// such as SMS verification codes, rate-limiting counters, etc.
package cache

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
)

// ============================================================
// SMS verify-code purpose constants
//
// 不同用途的校验码在 Redis 中用不同的 key 存储，互不覆盖。
// 有效时长也可按用途独立配置。
// ============================================================

const (
	// SMS4Login is used for user login verification.
	SMS4Login = "login"
	// SMS4Register is used for new user registration verification.
	SMS4Register = "regist"
	// SMS4PwdReset is used for password reset verification.
	SMS4PwdReset = "pwdreset"
)

// SMS4TTL maps each purpose to its verification code validity period.
var SMS4TTL = map[string]time.Duration{
	SMS4Login:    5 * time.Minute,
	SMS4Register: 10 * time.Minute,
	SMS4PwdReset: 10 * time.Minute,
}

const (
	// smsCodeKeyPrefix is the Redis key prefix for SMS codes.
	// Full key format: smscode:{purpose}:{tel}
	smsCodeKeyPrefix = "smscode:"

	// maxVerifyAttempts is the maximum number of failed verification attempts
	// before the code is automatically invalidated (防暴力破解).
	maxVerifyAttempts = 5

	// defaultTTL is used when no specific TTL is configured for a purpose.
	defaultTTL = 5 * time.Minute
)

// SmsCodeData represents the data stored in Redis for a verification code.
type SmsCodeData struct {
	Code          string `json:"code"`                      // 6-digit verification code
	Purpose       string `json:"purpose"`                   // "login", "regist", "pwdreset", etc.
	SentAt        string `json:"sent_at"`                   // ISO 8601 timestamp
	Attempts      int    `json:"attempts"`                  // Failed verification attempts
	Provider      string `json:"provider,omitempty"`        // SMS provider name, e.g. "aliyun"
	ProviderMsgID string `json:"provider_msg_id,omitempty"` // Provider's message ID
}

// SMSCodeCache wraps a Redis client for SMS verification code operations.
type SMSCodeCache struct {
	client *redis.Client
}

// NewSMSCodeCache creates a new SMSCodeCache using the given Redis client.
func NewSMSCodeCache(client *redis.Client) *SMSCodeCache {
	return &SMSCodeCache{client: client}
}

// smsCodeKey returns the Redis key for a given purpose and phone number.
// Format: smscode:{purpose}:{tel}
// Different purposes for the same tel DO NOT overwrite each other.
func smsCodeKey(purpose, tel string) string {
	return smsCodeKeyPrefix + purpose + ":" + tel
}

// getTTL returns the TTL for the given purpose, or defaultTTL if unknown.
func getTTL(purpose string) time.Duration {
	if ttl, ok := SMS4TTL[purpose]; ok {
		return ttl
	}
	return defaultTTL
}

// Generate generates a random 6-digit code for the given purpose and phone number,
// and stores it in Redis as a Hash. Returns the generated code.
// Different purposes are stored under different keys, so they never collide.
// provider is optional (empty string for development).
func (c *SMSCodeCache) Generate(ctx context.Context, purpose, tel string, provider ...string) (string, error) {
	code := generateRandomCode(6)
	key := smsCodeKey(purpose, tel)
	now := time.Now().UTC().Format(time.RFC3339)

	providerName := ""
	if len(provider) > 0 {
		providerName = provider[0]
	}

	data := map[string]interface{}{
		"code":     code,
		"purpose":  purpose,
		"sent_at":  now,
		"attempts": 0,
		"provider": providerName,
	}

	err := c.client.HSet(ctx, key, data).Err()
	if err != nil {
		return "", fmt.Errorf("redis: failed to set SMS code: %w", err)
	}

	ttl := getTTL(purpose)
	c.client.Expire(ctx, key, ttl)
	return code, nil
}

// Verify checks if the given code matches the stored code for the given purpose and tel.
// Returns true if the code is valid. On success, the code is consumed (deleted)
// to prevent replay attacks.
// Returns false if:
//   - no code exists or expired
//   - code doesn't match
//   - maxVerifyAttempts exceeded (auto-invalidated)
func (c *SMSCodeCache) Verify(ctx context.Context, purpose, tel, code string) bool {
	key := smsCodeKey(purpose, tel)

	// Read all hash fields
	data, err := c.client.HGetAll(ctx, key).Result()
	if err != nil || len(data) == 0 {
		return false // not found or expired
	}

	storedCode, ok := data["code"]
	if !ok {
		return false
	}

	// Check attempts
	attempts := 0
	if attemptsStr, ok := data["attempts"]; ok {
		fmt.Sscanf(attemptsStr, "%d", &attempts)
	}

	if attempts >= maxVerifyAttempts {
		// Too many failed attempts — delete and reject
		c.client.Del(ctx, key)
		return false
	}

	if storedCode != code {
		// Increment attempts
		c.client.HIncrBy(ctx, key, "attempts", 1)
		return false
	}

	// Code matched — consume it to prevent replay
	c.client.Del(ctx, key)
	return true
}

// GetData retrieves the full SMS code data for a given purpose and tel (without consuming it).
// Returns nil if no code exists or expired.
// Useful for debugging and admin purposes.
func (c *SMSCodeCache) GetData(ctx context.Context, purpose, tel string) (*SmsCodeData, error) {
	key := smsCodeKey(purpose, tel)

	data, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis: failed to get SMS code data: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	// Marshal to JSON then unmarshal to struct for convenience
	jsonBytes, _ := json.Marshal(data)
	var result SmsCodeData
	json.Unmarshal(jsonBytes, &result)

	return &result, nil
}

// generateRandomCode generates a cryptographically random N-digit code string.
func generateRandomCode(digits int) string {
	if digits <= 0 {
		digits = 6
	}

	code := make([]byte, digits)
	for i := 0; i < digits; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			// Fallback: should never happen on modern OS
			return fmt.Sprintf("%0*d", digits, time.Now().UnixNano()%1000000)
		}
		code[i] = byte('0' + n.Int64())
	}
	return string(code)
}
