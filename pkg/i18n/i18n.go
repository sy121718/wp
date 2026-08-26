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

// Init initializes i18n cache data and runtime behaviors from config.
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
			return fmt.Errorf("failed to load i18n cache: %w", err)
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

// SetDefaultLang sets default language code.
func SetDefaultLang(lang string) {
	initMu.Lock()
	defer initMu.Unlock()
	setDefaultLangLocked(lang)
}

// GetDefaultLang returns default language code.
func GetDefaultLang() string {
	initMu.Lock()
	defer initMu.Unlock()
	return defaultLang
}

// Get returns full i18n result.
//
// Example:
//
//	result := i18n.Get("ErrUploadConfigMissing", "zh-CN")
//	// result.Key      == "ErrUploadConfigMissing"
//	// result.Value    == "上传配置缺失"
//	// result.HttpCode == 400
//	// result.Lang     == "zh-CN"
//
// Fields:
//   - Key: code/text key
//   - Value: localized text
//   - Lang: matched language
//   - HttpCode: mapped HTTP status
//   - AllLangs: all language versions for this key
func Get(key, lang string) *I18nResult {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = GetDefaultLang()
	}
	return cache.Get(key, lang)
}

// GetText returns localized text only.
func GetText(key, lang string) string {
	result := Get(key, lang)
	if result == nil {
		return key
	}
	return result.Value
}

// GetHttpCode returns mapped HTTP status code for key.
func GetHttpCode(key string) int {
	result := Get(key, GetDefaultLang())
	if result == nil {
		return 200
	}
	return result.HttpCode
}

// Reload reloads i18n cache.
func Reload() error {
	if err := LoadCache(); err != nil {
		return err
	}

	initMu.Lock()
	inited = true
	initMu.Unlock()
	return nil
}

// IsInited reports whether i18n has been initialized.
func IsInited() bool {
	initMu.Lock()
	defer initMu.Unlock()
	return inited
}

// Close stops background refresh and resets runtime state.
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
			return initConfig{}, fmt.Errorf("failed to parse i18n refresh interval: %w", err)
		}
		cfg.refreshInterval = duration
	}

	return cfg, nil
}
