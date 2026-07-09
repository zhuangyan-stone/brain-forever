package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ============================================================
// Data models
// ============================================================

// CaptchaData 验证码的问题和点击区域答案。
type CaptchaData struct {
	QCn           string `json:"q_cn"`         // 中文提问
	QEn           string `json:"q_en"`         // 英文提问
	Left, Top     int    `json:"left,top"`     // 点击区域左上角
	Right, Bottom int    `json:"right,bottom"` // 点击区域右下角
}

// CaptchaItem 单个验证码条目。
type CaptchaItem struct {
	Image string      `json:"image"` // 图片 URL 相对路径，如 "d1/png/xxx.png"
	Data  CaptchaData `json:"data"`  // 验证码数据
}

// ============================================================
// CaptchaStore 接口
// ============================================================

// CaptchaStore 验证码数据的存储抽象，由调用方提供 Redis 实现。
type CaptchaStore interface {
	HSet(ctx context.Context, key, field string, value interface{}) error
	HGet(ctx context.Context, key, field string) (string, error)
	HRandField(ctx context.Context, key string, count int) ([]string, error)
	Del(ctx context.Context, key ...string) error
}

// ============================================================
// CaptchaProvider
// ============================================================

// CaptchaProvider 管理验证码数据的加载、获取和刷新。
type CaptchaProvider struct {
	captchaURLBase string       // 图片 URL 基础路径
	captchaDirBase string       // 本地文件系统路径
	activeDir      string       // 当前活动目录 "d1" 或 "d2"
	store          CaptchaStore // 存储后端（Redis 或 memory）
	mu             sync.RWMutex // 保护 activeDir，GetOne/Refresh 并发安全
}

// NewCaptchaProvider 创建并初始化 CaptchaProvider。
// 加载 d1 和 d2 的验证码数据到 store，检测 activeDir。
func NewCaptchaProvider(ctx context.Context, captchaURLBase, captchaDirBase string, store CaptchaStore) (*CaptchaProvider, error) {
	p := &CaptchaProvider{
		captchaURLBase: captchaURLBase,
		captchaDirBase: captchaDirBase,
		store:          store,
	}

	// 加载 d1 和 d2
	for _, dir := range []string{"d1", "d2"} {
		if err := p.loadAndStore(ctx, dir); err != nil {
			return nil, fmt.Errorf("failed to load captcha dir %q: %w", dir, err)
		}
	}

	// 检测 activeDir
	activeDir := "d1" // 默认
	if _, err := os.Stat(filepath.Join(captchaDirBase, "d1.active")); err == nil {
		activeDir = "d1"
	} else if _, err := os.Stat(filepath.Join(captchaDirBase, "d2.active")); err == nil {
		activeDir = "d2"
	}
	p.activeDir = activeDir

	return p, nil
}

// loadAndStore 读取指定目录下的 PNG 和 JSON 文件，将匹配的条目存入 store。
func (p *CaptchaProvider) loadAndStore(ctx context.Context, dir string) error {
	pngDir := filepath.Join(p.captchaDirBase, dir, "png")
	jsonDir := filepath.Join(p.captchaDirBase, dir, "json")

	// 读取所有 .png 文件，提取文件名（无扩展名）
	pngEntries, err := os.ReadDir(pngDir)
	if err != nil {
		return fmt.Errorf("cannot read png dir %q: %w", pngDir, err)
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

	// 读取所有 .json 文件到 map
	jsonEntries, err := os.ReadDir(jsonDir)
	if err != nil {
		return fmt.Errorf("cannot read json dir %q: %w", jsonDir, err)
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
			continue // 跳过无法读取的 JSON
		}
		jsonMap[baseName] = data
	}

	// 遍历 PNG 文件名，检查是否有对应 JSON
	redisKey := "CAPTCHAS_store." + dir
	for _, name := range pngNames {
		data, ok := jsonMap[name]
		if !ok {
			continue // 无对应 JSON，跳过
		}
		if err := p.store.HSet(ctx, redisKey, name, string(data)); err != nil {
			return fmt.Errorf("failed to hset captcha %q: %w", name, err)
		}
	}

	return nil
}

// ActiveDir 返回当前活动目录名称（"d1" 或 "d2"）。
func (p *CaptchaProvider) ActiveDir() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.activeDir
}

// GetOne 从当前活动目录中随机返回一个验证码条目。
func (p *CaptchaProvider) GetOne(ctx context.Context) (*CaptchaItem, error) {
	p.mu.RLock()
	activeDir := p.activeDir
	p.mu.RUnlock()

	redisKey := "CAPTCHAS_store." + activeDir

	// HRANDFIELD 取一个随机 field（图片名）
	fields, err := p.store.HRandField(ctx, redisKey, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to hrandfield from %q: %w", redisKey, err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no captcha available in %q", redisKey)
	}

	field := fields[0]

	// HGET 获取 JSON 数据
	val, err := p.store.HGet(ctx, redisKey, field)
	if err != nil {
		return nil, fmt.Errorf("failed to hget %q field %q: %w", redisKey, field, err)
	}

	var data CaptchaData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal captcha data for %q: %w", field, err)
	}

	return &CaptchaItem{
		Image: activeDir + "/png/" + field + ".png",
		Data:  data,
	}, nil
}

// Refresh 刷新指定目录的验证码数据到 store，并更新 activeDir。
func (p *CaptchaProvider) Refresh(ctx context.Context, activeDir string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldKey := "CAPTCHAS_store." + p.activeDir

	// 清空旧数据
	if err := p.store.Del(ctx, oldKey); err != nil {
		return fmt.Errorf("failed to del old captcha store %q: %w", oldKey, err)
	}

	// 重新加载
	if err := p.loadAndStore(ctx, activeDir); err != nil {
		return fmt.Errorf("failed to reload captcha dir %q: %w", activeDir, err)
	}

	p.activeDir = activeDir
	return nil
}
