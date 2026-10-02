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
	// defaultCountry / defaultCurrency 交易默认值（全局默认 + 兜底，读取方见
	// RuntimeValues 的字段注释）：与 defaultLang 同一套载入 / 刷新 / 失效机制。
	defaultCountry  = fallbackDefaultCountry
	defaultCurrency = fallbackDefaultCurrency
	// siteLangURLMode 站点访问路径的语言方案（默认 default_plain：默认语言无前缀
	// + 非默认语言短码前缀，见 langurl.go）。
	siteLangURLMode = SiteLangURLModeDefaultPlain
	// langURLCodeOverrides 语言码 → URL 短码的覆盖表（sys_config 的 i18n.lang_url_codes）。
	langURLCodeOverrides map[string]string
	// valueLoader 全局默认值的读取口（装配层注入；语义与刷新时机见 values.go）。
	// 未注入时三个默认值一律停在代码内常量上，不读 config.yaml。
	valueLoader ValueLoader
)

// initConfig 启动参数（**只含运行参数，不含业务默认值**）。
//
// 默认语言 / 站点语言 URL 方案 / 语言码覆盖不在其中：它们的唯一来源是 sys_config
// 的 i18n 组（见 values.go）。把业务默认值留在启动配置里就会造出第二个可改的地方，
// 而「改一次要重启、两处会漂移」正是本批要消灭的。
type initConfig struct {
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
	// 运行时状态复位到**代码内常量**：真正的值由装配层注入 loader 后载入
	// （见 values.go）。重复 Init（air 热重载 / 测试）时同样先复位，避免上一次
	// 进程生命期里的值残留成「第三个来源」。
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
	// 已注入 loader 时（测试 / 重复 Init）立即载入；未注入只留一条 INFO —— 装配层
	// 紧接着就会注入并即时载入，这里不要用告警把正常启动刷成"故障"。
	if err := refreshRuntimeValues(context.Background()); err != nil && errors.Is(err, errNoValueLoader) {
		logger.Info("i18n 全局默认值尚未接入配置源：当前使用代码内常量，装配层注入后立即生效")
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

// GetDefaultCountry 全局默认国家（ISO 3166-1 alpha-2）。
//
// 语义是「全局默认 + 兜底」：工程级覆盖仍走 projects.settings。
func GetDefaultCountry() string {
	initMu.Lock()
	defer initMu.Unlock()
	if defaultCountry == "" {
		return fallbackDefaultCountry
	}
	return defaultCountry
}

// GetDefaultCurrency 全局默认货币（ISO 4217）。返回值**保证非空**（未配置时回退代码内常量，
// 调用方不必再兜底）。
//
// **币种是标签，不是换算**：金额始终是数值（分），本值只决定这个数字代表哪种货币；
// 完整含义与运营风险（改了它 = 声明本站按该币种定价收款）见 RuntimeValues.DefaultCurrency。
//
// 读取方：SEO 的 priceCurrency、购物车金额展示、新建订单的 orders.currency 快照。
//
// 读的是**进程内缓存值**（由 i18n.SetValueLoader 在装配期载入、StartAutoRefresh 的 tick
// 与保存后的 Invalidate 刷新），因此可以在请求路径上直接调用 —— 不会每个请求查库。
func GetDefaultCurrency() string {
	initMu.Lock()
	defer initMu.Unlock()
	if defaultCurrency == "" {
		return fallbackDefaultCurrency
	}
	return defaultCurrency
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
	// 读取口一并清掉：Close 的语义是「回到未接入状态」，留着它会让下一次 Init 之前的
	// 读取仍然打到一个已关闭进程的依赖上（测试里表现为跨用例串值）。
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

// parseInitConfig 解析**启动参数**（自动刷新开关与间隔）。
//
// 这里刻意不再读 i18n.default_lang / site_lang_url_mode / lang_url_codes / site_lang_prefix：
// 那四项（最后一个是它的历史兼容键）的唯一来源是 sys_config 的 i18n 组，启动期由
// 装配层注入的 loader 载入（见 values.go）。保留读取就等于留了第二个可改的地方，
// 而两个来源迟早给出两个答案。解析函数 parseSiteLangURLMode 仍是本包内部实现，
// 被 loader 侧的应用逻辑与设置页校验共用。
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
