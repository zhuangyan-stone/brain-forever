package captcha

import (
	"context"
)

// ============================================================
// ICaptchaStore interface
// ============================================================

// ICaptchaStore is an abstraction for captcha data storage, with Redis implementation provided by the caller.
type ICaptchaStore interface {
	HSet(ctx context.Context, key, field string, value interface{}) error
	HGet(ctx context.Context, key, field string) (string, error)
	HRandField(ctx context.Context, key string, count int) ([]string, error)
	Del(ctx context.Context, key ...string) error
}
