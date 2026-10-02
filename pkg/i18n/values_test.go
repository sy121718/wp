package i18n

// values_test.go — 全局默认值单一来源的接入语义（阶段 2）。
//
// 这里钉的是「配置不可用时进程处在什么状态」这一类判断：它们错了不会报错，
// 只会让产物语言、URL 前缀方案悄悄变一个值（AGENTS.md 不变量 5 的失效形态）。

import (
	"context"
	"errors"
	"testing"
)

// resetRuntimeValues 每个用例前后回到「未注入、代码内常量」状态。
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

// TestValueLoaderAppliesImmediately 注入即生效：这是「保存后不用重启」的前提。
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

// TestValueLoaderFailureFallsBackToConstant 读取失败回退代码内常量：
// **不保留上一次的值**（否则进程状态取决于哪一次读成功了），也**不取空值**。
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

// TestValueLoaderEmptyValuesFallBack 配置组里缺键 / 值为空时按常量回退，绝不取空串。
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

// TestValueLoaderInvalidModeKeepsDefault 非法方案取值不阻断进程：按默认方案继续（并在
// applyRuntimeValues 里留下告警日志）。判断错了会让整个站点起不来或按非法值跑。
func TestValueLoaderInvalidModeKeepsDefault(t *testing.T) {
	resetRuntimeValues(t)

	SetValueLoader(func(context.Context) (RuntimeValues, error) {
		return RuntimeValues{DefaultLang: "zh-CN", SiteLangURLMode: "OFF"}, nil
	})

	if got := DefaultSiteLangURLMode(); got != SiteLangURLModeDefaultPlain {
		t.Fatalf("非法方案应按 default_plain 继续，实际 %q", got)
	}
}

// TestInvalidateRereadsValues 保存后主动刷新：Invalidate 让下一次读取取到新值。
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

// TestInvalidateWithoutLoaderIsNoop 未注入配置源时 Invalidate 是空操作
// （`-migrate-only` 这类不装配路由的进程会走到这里），且不得把状态改成空值。
func TestInvalidateWithoutLoaderIsNoop(t *testing.T) {
	resetRuntimeValues(t)

	Invalidate()

	if got := GetDefaultLang(); got != fallbackDefaultLang {
		t.Fatalf("无 loader 时默认语言 = %q，期望常量 %q", got, fallbackDefaultLang)
	}
}

// TestCloseDetachesLoader Close 之后回到「未接入」状态：残留的 loader 会让下一次 Init
// 之前的读取仍打到已关闭进程的依赖上（测试里表现为跨用例串值）。
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
