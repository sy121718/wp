package i18n

import (
	"context"
	"errors"
	"testing"
)

func resetRuntimeValues(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		initMu.Lock()
		valueLoader = nil
		defaultLang = fallbackDefaultLang
		siteLangURLMode = SiteLangURLModeDefaultPlain
		langURLCodeOverrides = nil
		initMu.Unlock()
	})
}

func TestValueLoaderAppliesImmediately(t *testing.T) {
	resetRuntimeValues(t)

	SetValueLoader(func(context.Context) (RuntimeValues, error) {
		return RuntimeValues{
			DefaultLang:     "en-US",
			SiteLangURLMode: string(SiteLangURLModeAllPrefix),
			LangURLCodes:    map[string]string{"en-AU": "en"},
		}, nil
	})

	if got := GetDefaultLang(); got != "en-US" {
		t.Fatalf("注入后默认语言 = %q，期望 en-US", got)
	}
	if got := DefaultSiteLangURLMode(); got != SiteLangURLModeAllPrefix {
		t.Fatalf("注入后 URL 方案 = %q，期望 all_prefix", got)
	}
	if got := URLCodeOverrides(); got["en-AU"] != "en" {
		t.Fatalf("注入后语言码覆盖 = %v，期望含 en-AU→en", got)
	}
}

func TestValueLoaderFailureFallsBackToConstant(t *testing.T) {
	resetRuntimeValues(t)

	SetValueLoader(func(context.Context) (RuntimeValues, error) {
		return RuntimeValues{DefaultLang: "en-US", SiteLangURLMode: string(SiteLangURLModeAllPrefix)}, nil
	})
	if GetDefaultLang() != "en-US" {
		t.Fatalf("前置条件不成立：默认语言 = %q", GetDefaultLang())
	}

	boom := errors.New("库不可用")
	SetValueLoader(func(context.Context) (RuntimeValues, error) { return RuntimeValues{}, boom })

	if got := GetDefaultLang(); got != fallbackDefaultLang {
		t.Fatalf("读取失败后默认语言 = %q，期望回退常量 %q", got, fallbackDefaultLang)
	}
	if got := DefaultSiteLangURLMode(); got != SiteLangURLModeDefaultPlain {
		t.Fatalf("读取失败后 URL 方案 = %q，期望默认方案", got)
	}
}

func TestValueLoaderEmptyValuesFallBack(t *testing.T) {
	resetRuntimeValues(t)

	SetValueLoader(func(context.Context) (RuntimeValues, error) {
		return RuntimeValues{DefaultLang: "   ", SiteLangURLMode: ""}, nil
	})

	if got := GetDefaultLang(); got != fallbackDefaultLang {
		t.Fatalf("空默认语言应回退常量，实际 %q", got)
	}
	if got := DefaultSiteLangURLMode(); got != SiteLangURLModeDefaultPlain {
		t.Fatalf("空方案应回退 default_plain，实际 %q", got)
	}
	if got := URLCodeOverrides(); got != nil {
		t.Fatalf("无覆盖时应为 nil，实际 %v", got)
	}
}

func TestValueLoaderInvalidModeKeepsDefault(t *testing.T) {
	resetRuntimeValues(t)

	SetValueLoader(func(context.Context) (RuntimeValues, error) {
		return RuntimeValues{DefaultLang: "zh-CN", SiteLangURLMode: "OFF"}, nil
	})

	if got := DefaultSiteLangURLMode(); got != SiteLangURLModeDefaultPlain {
		t.Fatalf("非法方案应按 default_plain 继续，实际 %q", got)
	}
}

func TestInvalidateRereadsValues(t *testing.T) {
	resetRuntimeValues(t)

	lang := "zh-CN"
	SetValueLoader(func(context.Context) (RuntimeValues, error) {
		return RuntimeValues{DefaultLang: lang}, nil
	})
	if GetDefaultLang() != "zh-CN" {
		t.Fatalf("前置条件不成立：默认语言 = %q", GetDefaultLang())
	}

	lang = "ja-JP"
	Invalidate()
	if got := GetDefaultLang(); got != "ja-JP" {
		t.Fatalf("Invalidate 后默认语言 = %q，期望 ja-JP", got)
	}
}

func TestInvalidateWithoutLoaderIsNoop(t *testing.T) {
	resetRuntimeValues(t)

	Invalidate()

	if got := GetDefaultLang(); got != fallbackDefaultLang {
		t.Fatalf("无 loader 时默认语言 = %q，期望常量 %q", got, fallbackDefaultLang)
	}
}

func TestCloseDetachesLoader(t *testing.T) {
	resetRuntimeValues(t)

	SetValueLoader(func(context.Context) (RuntimeValues, error) {
		return RuntimeValues{DefaultLang: "en-US"}, nil
	})
	if err := Close(); err != nil {
		t.Fatalf("Close 报错：%v", err)
	}
	if err := refreshRuntimeValues(context.Background()); !errors.Is(err, errNoValueLoader) {
		t.Fatalf("Close 后读取应返回未注入，实际 %v", err)
	}
	if got := GetDefaultLang(); got != fallbackDefaultLang {
		t.Fatalf("Close 后默认语言 = %q，期望常量 %q", got, fallbackDefaultLang)
	}
}
