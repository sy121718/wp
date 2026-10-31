package i18n

import (
	"fmt"
	"strings"
)

type SiteLangURLMode string

const (
	SiteLangURLModeOff SiteLangURLMode = "off"

	SiteLangURLModeDefaultPlain SiteLangURLMode = "default_plain"

	SiteLangURLModeAllPrefix SiteLangURLMode = "all_prefix"
)

func parseSiteLangURLMode(raw string) (SiteLangURLMode, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return SiteLangURLModeDefaultPlain, nil
	case string(SiteLangURLModeOff):
		return SiteLangURLModeOff, nil
	case string(SiteLangURLModeDefaultPlain):
		return SiteLangURLModeDefaultPlain, nil
	case string(SiteLangURLModeAllPrefix):
		return SiteLangURLModeAllPrefix, nil
	default:
		return "", fmt.Errorf("站点语言 URL 方案取值非法: %q（可选 off / default_plain / all_prefix；配置位置：系统配置的 i18n 组 site_lang_url_mode）", raw)
	}
}

func ParseSiteLangURLMode(raw string) (SiteLangURLMode, error) {
	return parseSiteLangURLMode(raw)
}

func DefaultSiteLangURLMode() SiteLangURLMode {
	initMu.Lock()
	defer initMu.Unlock()
	return siteLangURLMode
}

func SiteLangURLsSeparated(mode SiteLangURLMode) bool {
	return mode != SiteLangURLModeOff
}

func SiteLangURLPrefixDefault(mode SiteLangURLMode) bool {
	return mode == SiteLangURLModeAllPrefix
}

func SetURLCodeOverrides(codes map[string]string) {
	initMu.Lock()
	defer initMu.Unlock()
	if len(codes) == 0 {
		langURLCodeOverrides = nil
		return
	}
	cp := make(map[string]string, len(codes))
	for k, v := range codes {
		cp[k] = v
	}
	langURLCodeOverrides = cp
}

func URLCodeOverrides() map[string]string {
	initMu.Lock()
	defer initMu.Unlock()
	if len(langURLCodeOverrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(langURLCodeOverrides))
	for k, v := range langURLCodeOverrides {
		out[k] = v
	}
	return out
}
