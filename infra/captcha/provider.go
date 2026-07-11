package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"BrainForever/infra/zylog"
)

// ============================================================
// Data models
// ============================================================

// CaptchaData holds the captcha question and click area answer.
type CaptchaData struct {
	QCn string `json:"q-cn"` // Chinese question
	QEn string `json:"q-en"` // English question
	A   [4]int `json:"a"`    // Click area [left, top, right, bottom]
}

// CaptchaItem represents a single captcha entry.
type CaptchaItem struct {
	Image string      `json:"image"` // Image URL relative path, e.g. "d1/png/xxx.png"
	Data  CaptchaData `json:"data"`  // Captcha data
}

// ============================================================
// CaptchaProvider
// ============================================================

// CaptchaProvider manages loading, retrieval, and refreshing of captcha data.
type CaptchaProvider struct {
	captchaURLBase string        // Base URL for captcha images
	captchaDirBase string        // Local filesystem path
	activeDir      string        // Current active directory "d1" or "d2"
	activeCount    int           // Number of loaded images in the active directory
	store          ICaptchaStore // Storage backend (Redis or memory)
	logger         zylog.Logger  // Logger for captcha operations
	mu             sync.RWMutex  // Protects activeDir/activeCount, ensures GetOne/Refresh concurrency safety
}

// NewCaptchaProvider creates and initializes a CaptchaProvider.
// Loads captcha data from d1 and d2 into the store, detects activeDir.
func NewCaptchaProvider(ctx context.Context, captchaURLBase, captchaDirBase string, store ICaptchaStore, logger zylog.Logger) (*CaptchaProvider, error) {
	p := &CaptchaProvider{
		captchaURLBase: captchaURLBase,
		captchaDirBase: captchaDirBase,
		store:          store,
		logger:         logger,
	}

	// Load d1 and d2, track the count for the active directory
	var activeCount int
	for _, dir := range []string{"d1", "d2"} {
		// Delete stale entries first to avoid accumulating orphaned captcha
		// entries in Redis when PNG files are removed from disk between restarts.
		oldKey := "CAPTCHAS_store." + dir
		if err := store.Del(ctx, oldKey); err != nil {
			return nil, fmt.Errorf("failed to clear stale captcha store %q: %w", oldKey, err)
		}

		count, err := p.loadAndStore(ctx, dir)
		if err != nil {
			return nil, fmt.Errorf("failed to load captcha dir %q: %w", dir, err)
		}
		// Detect activeDir while iterating
		if _, err := os.Stat(filepath.Join(captchaDirBase, dir+".active")); err == nil {
			p.activeDir = dir
			activeCount = count
		}
	}

	// Ensure activeDir has a default value
	if p.activeDir == "" {
		p.activeDir = "d1"
	}
	p.activeCount = activeCount

	return p, nil
}

// loadAndStore reads PNG and JSON files in the specified directory and stores matching entries into the store.
// If the png or json subdirectories do not exist, they are created automatically.
// Returns the number of successfully stored entries.
func (p *CaptchaProvider) loadAndStore(ctx context.Context, dir string) (int, error) {
	pngDir := filepath.Join(p.captchaDirBase, dir, "png")
	jsonDir := filepath.Join(p.captchaDirBase, dir, "json")

	// Ensure png subdirectory exists
	if err := os.MkdirAll(pngDir, 0755); err != nil {
		return 0, fmt.Errorf("cannot create png dir %q: %w", pngDir, err)
	}

	// Read all .png files, extract filenames (without extension)
	pngEntries, err := os.ReadDir(pngDir)
	if err != nil {
		return 0, fmt.Errorf("cannot read png dir %q: %w", pngDir, err)
	}

	var pngNames []string
	for _, entry := range pngEntries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".png") {
			continue
		}
		pngNames = append(pngNames, strings.TrimSuffix(name, ".png"))
	}

	// Ensure json subdirectory exists
	if err := os.MkdirAll(jsonDir, 0755); err != nil {
		return 0, fmt.Errorf("cannot create json dir %q: %w", jsonDir, err)
	}

	// Read all .json files into a map
	jsonEntries, err := os.ReadDir(jsonDir)
	if err != nil {
		return 0, fmt.Errorf("cannot read json dir %q: %w", jsonDir, err)
	}

	jsonMap := make(map[string][]byte, len(jsonEntries))
	for _, entry := range jsonEntries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		baseName := strings.TrimSuffix(name, ".json")
		data, err := os.ReadFile(filepath.Join(jsonDir, name))
		if err != nil {
			continue // skip unreadable JSON
		}
		jsonMap[baseName] = data
	}

	// Iterate over PNG filenames and check for corresponding JSON
	redisKey := "CAPTCHAS_store." + dir
	count := 0
	for _, name := range pngNames {
		data, ok := jsonMap[name]
		if !ok {
			continue // no matching JSON, skip
		}
		if err := p.store.HSet(ctx, redisKey, name, string(data)); err != nil {
			return count, fmt.Errorf("failed to hset captcha %q: %w", name, err)
		}
		count++
	}

	return count, nil
}

// ActiveDir returns the current active directory name ("d1" or "d2").
func (p *CaptchaProvider) ActiveDir() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.activeDir
}

// ActiveCount returns the number of loaded images in the active directory.
func (p *CaptchaProvider) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.activeCount
}

// GetOne returns a random captcha entry from the current active directory.
func (p *CaptchaProvider) GetOne(ctx context.Context) (*CaptchaItem, error) {
	p.mu.RLock()
	activeDir := p.activeDir
	p.mu.RUnlock()

	redisKey := "CAPTCHAS_store." + activeDir

	// HRANDFIELD gets a random field (image name)
	fields, err := p.store.HRandField(ctx, redisKey, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to hrandfield from %q: %w", redisKey, err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no captcha available in %q", redisKey)
	}

	field := fields[0]

	// HGET retrieves JSON data
	val, err := p.store.HGet(ctx, redisKey, field)
	if err != nil {
		return nil, fmt.Errorf("failed to hget %q field %q: %w", redisKey, field, err)
	}

	var data CaptchaData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal captcha data for %q: %w", field, err)
	}

	return &CaptchaItem{
		Image: p.captchaURLBase + activeDir + "/png/" + field + ".png",
		Data:  data,
	}, nil
}

// Refresh reloads captcha data for the specified directory into the store and updates activeDir.
func (p *CaptchaProvider) Refresh(ctx context.Context, activeDir string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldKey := "CAPTCHAS_store." + p.activeDir

	// Clear old data
	if err := p.store.Del(ctx, oldKey); err != nil {
		return fmt.Errorf("failed to del old captcha store %q: %w", oldKey, err)
	}

	// Reload
	count, err := p.loadAndStore(ctx, activeDir)
	if err != nil {
		return fmt.Errorf("failed to reload captcha dir %q: %w", activeDir, err)
	}

	p.activeDir = activeDir
	p.activeCount = count

	p.logger.Infof("captcha provider refreshed (activeDir=%s, count=%d)", p.activeDir, p.activeCount)
	return nil
}
