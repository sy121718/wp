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
	// siteLangURLMode 站点访问路径的语言方案（默认 default_plain：默认语言无前缀
	// + 非默认语言短码前缀，见 langurl.go）。
	siteLangURLMode = SiteLangURLModeDefaultPlain
	// langURLCodeOverrides 语言码 → URL 短码的配置覆盖表（i18n.lang_url_codes）。
	langURLCodeOverrides map[string]string
)

type initConfig struct {
	defaultLang     string
	autoRefresh     bool
	refreshInterval time.Duration
	// siteURLMode 站点访问路径的语言方案（多语言 P2/P3 的落地开关）。
	siteURLMode SiteLangURLMode
	// urlCodes 语言码 → URL 短码的配置覆盖表。
	urlCodes map[string]string
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
	siteLangURLMode = cfg.siteURLMode
	langURLCodeOverrides = cfg.urlCodes
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

// SiteLangPrefixEnabled 兼容名：报告站点是否按语言区分访问路径。
//
// 语义已随方案调整（docs/06-D §5 方案 A'）：默认方案 default_plain 下**默认语言
// 无前缀**、非默认语言带短码前缀，因此「是否分离语言路径」不再等于「是否全带
// 前缀」。新代码请用 SiteLangURLsSeparated / SiteLangURLModeValue。
func SiteLangPrefixEnabled() bool {
	return SiteLangURLsSeparated()
}

// SetSiteLangPrefix 兼容名：true → all_prefix（全语言带短码前缀），
// false → off（全语言共用逻辑路径）。新代码请用 SetSiteLangURLMode。
func SetSiteLangPrefix(enabled bool) {
	if enabled {
		SetSiteLangURLMode(SiteLangURLModeAllPrefix)
		return
	}
	SetSiteLangURLMode(SiteLangURLModeOff)
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
	siteLangURLMode = SiteLangURLModeDefaultPlain
	langURLCodeOverrides = nil
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
	// 语言 URL 方案：优先 i18n.site_lang_url_mode（枚举）；缺省时回退旧键
	// i18n.site_lang_prefix（true → all_prefix / false → off）；两者都没有则
	// 采用默认方案 default_plain（默认语言无前缀 + 非默认语言短码）。
	modeRaw := strings.TrimSpace(v.GetString("i18n.site_lang_url_mode"))
	if modeRaw == "" && v.IsSet("i18n.site_lang_prefix") {
		if v.GetBool("i18n.site_lang_prefix") {
			modeRaw = string(SiteLangURLModeAllPrefix)
		} else {
			modeRaw = string(SiteLangURLModeOff)
		}
	}
	mode, err := parseSiteLangURLMode(modeRaw)
	if err != nil {
		return initConfig{}, err
	}
	cfg.siteURLMode = mode
	if codes := v.GetStringMapString("i18n.lang_url_codes"); len(codes) > 0 {
		cfg.urlCodes = codes
	}

	if raw := strings.TrimSpace(v.GetString("i18n.refresh_interval")); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil {
			return initConfig{}, fmt.Errorf("failed to parse i18n refresh interval: %w", err)
		}
		cfg.refreshInterval = duration
	}

	return cfg, nil
}
