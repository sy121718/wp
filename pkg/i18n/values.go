package i18n

// values.go — 全局默认值的**单一来源**接入点（默认语言 / 站点语言 URL 方案 / 语言码覆盖）。
//
// 为什么要有这一层：这三项原先来自 config.yaml，改一次就得重启，且散落在多处解析
// （parseInitConfig 读四个键、站点设置页另有一套热更新）。现在唯一来源是 sys_config 的
// i18n 组，本包只声明「怎么取」的形状，**不认识来源**：
//
//   - pkg 不 import internal/module/**（分层）：pkg 只持有回调类型 ValueLoader；
//   - 装配层（internal/routers）初始化 sysconfig 后把它的**只读窄口**适配成 ValueLoader
//     注入（依赖方向 internal → pkg），见 internal/module/sysconfig/outbound/i18nvalues；
//   - DB 未就绪 / 未注入 / 读取失败时，一律退回**代码内常量** fallbackDefaultLang 与
//     默认方案 default_plain，并记日志。常量不可配置，因此不构成「第二个来源」。
//
// 刷新时机只有两个，刻意不新增第二套机制：
//  1. 注入时立即载入一次（SetValueLoader），启动后即取到 DB 值；
//  2. 复用既有 StartAutoRefresh 的 tick 顺带刷新（loader.go），外加保存后的 Invalidate。

import (
	"context"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/logger"
)

// valueLoadTimeout 单次读取超时。
//
// 读取发生在装配期（阻塞启动）与后台 tick 上：没有超时的话，一次库故障会把启动
// 或刷新 goroutine 挂住 —— 配置读不到是「用兜底值跑」的场景，不该变成「起不来」。
const valueLoadTimeout = 5 * time.Second

// errNoValueLoader 未注入配置源（测试装配、`-migrate-only` 这类不装配路由的进程）。
// 它不是故障：那些进程没有消费方；调用方据此避免刷屏日志。
var errNoValueLoader = errors.New("i18n 配置源未注入")

// RuntimeValues 一次读取到的全局默认值（零值 = 该键没有配，按代码内常量回退）。
type RuntimeValues struct {
	// DefaultLang 全局默认语言（完整语言码）；空 = 未配置。
	DefaultLang string
	// SiteLangURLMode 站点语言 URL 方案；空 = 未配置（按 default_plain）。
	SiteLangURLMode string
	// LangURLCodes 语言码 → URL 短码覆盖表；空 = 无覆盖。
	LangURLCodes map[string]string
}

// ValueLoader 取一次全局默认值的回调。
//
// pkg 侧只定义形状、不定义来源（见文件头）：装配层把 sysconfig 的 ConfigReader 适配进来。
type ValueLoader func(ctx context.Context) (RuntimeValues, error)

// SetValueLoader 注入全局默认值的读取口，并立即载入一次。
//
// 重复注入会替换掉旧实现（装配在一个进程内只发生一次；重复装配的测试里后者胜出，
// 与其它装配期 setter 同口径）。
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

// Invalidate 让进程内缓存的全局默认值立即失效并重新读取。
//
// 调用时机只有一个：配置**保存成功之后**（装配层把本函数注入 sysconfig service 的
// onChanged）。不做主动刷新的话，「保存了但不生效」要等下一次 tick 才恢复 —— 那正是
// 配置进了库、行为却还是旧的口径分叉。
//
// 未注入配置源的进程是空操作：没有消费方需要刷新，也不该因为「没注入」而报错。
func Invalidate() {
	err := refreshRuntimeValues(context.Background())
	switch {
	case err == nil, errors.Is(err, errNoValueLoader):
		return
	default:
		logger.With("err", err.Error()).Warn("i18n 全局默认值刷新失败：本次沿用代码内常量")
	}
}

// refreshRuntimeValues 取一次值并应用；未注入返回 errNoValueLoader。
//
// 读取失败**不保留上一次的值**，而是回退到代码内常量：失败意味着「当前配置不可知」，
// 继续用上一次的值会让进程状态取决于「哪一次读成功了」这种不可复现的时序，
// 而默认语言是进产物字节的（AGENTS.md 不变量 5）。
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

// applyRuntimeValues 把读到的一组值应用到进程状态（零值按代码内常量回退）。
//
// 三种「配置不可用」都必须在**同一处**收敛，否则它们会各自演化成不同的默认：
// 缺键 / 空值 → 常量；取值非法 → 默认方案 + 日志（不阻断进程 —— 一个写错的枚举值
// 不该让站点起不来，但也不能静默按非法值继续跑）；显式 off → 默认方案以外的**合法**
// 值，只记告警（语言切换器不会渲染，这是用户可见的功能缺失，必须留痕）。
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
}

// copyURLCodes 复制覆盖表（空表归零）：调用方持有的 map 之后被改，不该影响已生效的映射。
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

// defaultLangSnapshot 读当前默认语言（日志用，避免在持锁路径里调 logger）。
func defaultLangSnapshot() string {
	initMu.Lock()
	defer initMu.Unlock()
	return defaultLang
}
