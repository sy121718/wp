package i18n

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"go_wp/pkg/logger"
)

const fallbackDefaultLang string = "zh-CN"

var (
	initMu      sync.Mutex
	inited      bool
	defaultLang = fallbackDefaultLang

	defaultCountry  = fallbackDefaultCountry
	defaultCurrency = fallbackDefaultCurrency

	siteLangURLMode = SiteLangURLModeDefaultPlain

	langURLCodeOverrides map[string]string

	valueLoader ValueLoader
)

type initConfig struct {
	autoRefresh     bool
	refreshInterval time.Duration
}

func Init(v *viper.Viper) error {
	cfg, err := parseInitConfig(v)
	if err != nil {
		return err
	}

	initMu.Lock()
	alreadyInited := inited

	defaultLang = fallbackDefaultLang
	siteLangURLMode = SiteLangURLModeDefaultPlain
	langURLCodeOverrides = nil
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

	if err := refreshRuntimeValues(context.Background()); err != nil && errors.Is(err, errNoValueLoader) {
		logger.Info("i18n 全局默认值尚未接入配置源：当前使用代码内常量，装配层注入后立即生效")
	}
	return nil
}

func SetDefaultLang(lang string) {
	initMu.Lock()
	defer initMu.Unlock()
	setDefaultLangLocked(lang)
}

func GetDefaultLang() string {
	initMu.Lock()
	defer initMu.Unlock()
	return defaultLang
}

func GetDefaultCountry() string {
	initMu.Lock()
	defer initMu.Unlock()
	if defaultCountry == "" {
		return fallbackDefaultCountry
	}
	return defaultCountry
}

func GetDefaultCurrency() string {
	initMu.Lock()
	defer initMu.Unlock()
	if defaultCurrency == "" {
		return fallbackDefaultCurrency
	}
	return defaultCurrency
}

func Get(key, lang string) *I18nResult {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = GetDefaultLang()
	}
	return cache.Get(key, lang)
}

func GetText(key, lang string) string {
	result := Get(key, lang)
	if result == nil {
		return key
	}
	return result.Value
}

func GetHttpCode(key string) int {
	result := Get(key, GetDefaultLang())
	if result == nil {
		return 200
	}
	return result.HttpCode
}

func Reload() error {
	if err := LoadCache(); err != nil {
		return err
	}

	initMu.Lock()
	inited = true
	initMu.Unlock()
	return nil
}

func IsInited() bool {
	initMu.Lock()
	defer initMu.Unlock()
	return inited
}

func Close() error {
	StopAutoRefresh()

	initMu.Lock()
	inited = false
	defaultLang = fallbackDefaultLang
	siteLangURLMode = SiteLangURLModeDefaultPlain
	langURLCodeOverrides = nil

	valueLoader = nil
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
		autoRefresh:     false,
		refreshInterval: 20 * time.Second,
	}
	if v == nil {
		return cfg, nil
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
