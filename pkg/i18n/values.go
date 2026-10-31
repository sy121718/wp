package i18n

import (
	"context"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/logger"
)

const valueLoadTimeout = 5 * time.Second

var errNoValueLoader = errors.New("i18n 配置源未注入")

type RuntimeValues struct {
	DefaultLang string

	SiteLangURLMode string

	LangURLCodes map[string]string

	DefaultCountry string

	DefaultCurrency string
}

type ValueLoader func(ctx context.Context) (RuntimeValues, error)

func SetValueLoader(fn ValueLoader) {
	initMu.Lock()
	valueLoader = fn
	initMu.Unlock()

	if fn == nil {
		return
	}
	if err := refreshRuntimeValues(context.Background()); err != nil {
		logger.With("err", err.Error()).Warn("i18n 全局默认值载入失败：本次沿用代码内常量，等待下一次刷新")
		return
	}
	logger.WithFields(map[string]any{
		"defaultLang":  defaultLangSnapshot(),
		"siteLangMode": string(DefaultSiteLangURLMode()),
	}).Info("i18n 全局默认值已从配置源载入")
}

func Invalidate() {
	err := refreshRuntimeValues(context.Background())
	switch {
	case err == nil, errors.Is(err, errNoValueLoader):
		return
	default:
		logger.With("err", err.Error()).Warn("i18n 全局默认值刷新失败：本次沿用代码内常量")
	}
}

func refreshRuntimeValues(ctx context.Context) error {
	initMu.Lock()
	loader := valueLoader
	initMu.Unlock()
	if loader == nil {
		return errNoValueLoader
	}

	if ctx == nil {
		ctx = context.Background()
	}
	loadCtx, cancel := context.WithTimeout(ctx, valueLoadTimeout)
	defer cancel()

	vals, err := loader(loadCtx)
	if err != nil {
		applyRuntimeValues(RuntimeValues{})
		return err
	}
	applyRuntimeValues(vals)
	return nil
}

func applyRuntimeValues(vals RuntimeValues) {
	mode := SiteLangURLModeDefaultPlain
	modeErr := error(nil)
	if raw := strings.TrimSpace(vals.SiteLangURLMode); raw != "" {
		parsed, perr := parseSiteLangURLMode(raw)
		if perr != nil {
			modeErr = perr
		} else {
			mode = parsed
		}
	}

	initMu.Lock()
	setDefaultLangLocked(vals.DefaultLang)
	prevMode := siteLangURLMode
	siteLangURLMode = mode
	langURLCodeOverrides = copyURLCodes(vals.LangURLCodes)
	initMu.Unlock()

	if strings.TrimSpace(vals.DefaultLang) == "" {
		logger.With("fallback", fallbackDefaultLang).
			Warn("i18n 默认语言未配置或为空，回退代码内常量")
	}
	if modeErr != nil {
		logger.With("raw", vals.SiteLangURLMode).With("fallback", string(SiteLangURLModeDefaultPlain)).
			Warn("i18n 站点语言 URL 方案取值非法，按默认方案继续（该值不会被自动纠正，请在系统配置里改对）")
	}
	if mode == SiteLangURLModeOff && prevMode != SiteLangURLModeOff {
		logger.With("mode", string(SiteLangURLModeOff)).Warn(
			"站点语言 URL 方案为 off：各语言映射到同一路径，语言切换器不会渲染；启用多种语言时请改用 default_plain 或 all_prefix")
	}

	initMu.Lock()
	defaultCountry = strings.TrimSpace(vals.DefaultCountry)
	if defaultCountry == "" {
		defaultCountry = fallbackDefaultCountry
	}
	defaultCurrency = strings.TrimSpace(vals.DefaultCurrency)
	if defaultCurrency == "" {
		defaultCurrency = fallbackDefaultCurrency
	}
	initMu.Unlock()
}

const (
	fallbackDefaultCountry  = "CN"
	fallbackDefaultCurrency = "CNY"
)

func copyURLCodes(codes map[string]string) map[string]string {
	if len(codes) == 0 {
		return nil
	}
	cp := make(map[string]string, len(codes))
	for k, v := range codes {
		cp[k] = v
	}
	return cp
}

func defaultLangSnapshot() string {
	initMu.Lock()
	defer initMu.Unlock()
	return defaultLang
}
