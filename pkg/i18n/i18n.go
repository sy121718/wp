package i18n

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

const fallbackDefaultLang string = "zh-CN"

var (
	initMu      sync.Mutex
	inited      bool
	defaultLang = fallbackDefaultLang
)

type initConfig struct {
	defaultLang     string
	autoRefresh     bool
	refreshInterval time.Duration
}

// Init 初始化 i18n 缓存数据和运行时行为。
func Init(v *viper.Viper) error {
	cfg, err := parseInitConfig(v)
	if err != nil {
		return err
	}

	initMu.Lock()
	alreadyInited := inited
	setDefaultLangLocked(cfg.defaultLang)
	initMu.Unlock()

	if !alreadyInited {
		if err := LoadCache(); err != nil {
			return fmt.Errorf("i18n 缓存加载失败: %w", err)
		}

		initMu.Lock()
		inited = true
		initMu.Unlock()
	}

	StopAutoRefresh()
	if cfg.autoRefresh {
		StartAutoRefresh(cfg.refreshInterval)
	}
	return nil
}

// SetDefaultLang 设置默认语言代码。
func SetDefaultLang(lang string) {
	initMu.Lock()
	defer initMu.Unlock()
	setDefaultLangLocked(lang)
}

// GetDefaultLang 返回默认语言代码。
func GetDefaultLang() string {
	initMu.Lock()
	defer initMu.Unlock()
	return defaultLang
}

// Get 返回完整的 i18n 查询结果。
//
// 示例:
//
//	result := i18n.Get("ErrUploadConfigMissing", "zh-CN")
//	// result.Key      == "ErrUploadConfigMissing"
//	// result.Value    == "上传配置缺失"
//	// result.HttpCode == 400
//	// result.Lang     == "zh-CN"
func Get(key, lang string) *I18nResult {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = GetDefaultLang()
	}
	return cache.Get(key, lang)
}

// GetText 只返回本地化文本。
func GetText(key, lang string) string {
	result := Get(key, lang)
	if result == nil {
		return key
	}
	return result.Value
}

// GetHttpCode 返回 key 对应的 HTTP 状态码。
func GetHttpCode(key string) int {
	result := Get(key, GetDefaultLang())
	if result == nil {
		return 200
	}
	return result.HttpCode
}

// Reload 重新加载 i18n 缓存。
func Reload() error {
	if err := LoadCache(); err != nil {
		return err
	}

	initMu.Lock()
	inited = true
	initMu.Unlock()
	return nil
}

// IsInited 报告 i18n 是否已完成初始化。
func IsInited() bool {
	initMu.Lock()
	defer initMu.Unlock()
	return inited
}

// Close 停止后台刷新并重置运行时状态。
func Close() error {
	StopAutoRefresh()

	initMu.Lock()
	inited = false
	defaultLang = fallbackDefaultLang
	initMu.Unlock()
	return nil
}

func setDefaultLangLocked(lang string) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		defaultLang = fallbackDefaultLang
		return
	}

	defaultLang = lang
}

func parseInitConfig(v *viper.Viper) (initConfig, error) {
	cfg := initConfig{
		defaultLang:     fallbackDefaultLang,
		autoRefresh:     false,
		refreshInterval: 20 * time.Second,
	}
	if v == nil {
		return cfg, nil
	}

	if lang := strings.TrimSpace(v.GetString("i18n.default_lang")); lang != "" {
		cfg.defaultLang = lang
	}
	cfg.autoRefresh = v.GetBool("i18n.auto_refresh")

	if raw := strings.TrimSpace(v.GetString("i18n.refresh_interval")); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil {
			return initConfig{}, fmt.Errorf("i18n 刷新间隔解析失败: %w", err)
		}
		cfg.refreshInterval = duration
	}

	return cfg, nil
}