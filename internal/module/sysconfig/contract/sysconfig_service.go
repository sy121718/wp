// Package sysconfigcontract 定义 sysconfig 模块对外契约（跨模块可见的东西都放这里）。
//
// 三段：对外能力（Service）· 跨模块形状（dto 的重导出）· 索要的端口（无）。
package sysconfigcontract

import (
	"context"
	"errors"

	sysconfigdto "go_wp/internal/module/sysconfig/dto"
)

// === 对外能力 ===

// Service 系统配置的读写能力。
type Service interface {
	// GetGroup 取一组配置（不存在返回 GroupNotFound）。
	GetGroup(ctx context.Context, groupKey string) (*Group, error)
	// ListGroups 列出全部分组（只读，按分组键排序）。
	ListGroups(ctx context.Context) ([]Group, error)
	// SetGroup 整组保存（乐观锁：带读到的 Version，库内已变则整组拒绝）。
	SetGroup(ctx context.Context, req *SetGroupReq) (*Group, error)
	// SetGroups 多组一次保存（**单事务** + 逐组乐观锁）：后台一张表单同时改两组时用，
	// 避免「第一组改了、第二组失败」的半截状态。
	SetGroups(ctx context.Context, req *SetGroupsReq) ([]Group, error)
}

// DictReader 数据字典**只读口**（窄接口）：只够后台页面给下拉 / datalist 供数。
//
// 为什么是独立一条而不是并进 Service：写能力（SetGroup）与读字典是两条边界 ——
// 后台页面只要读；把整条 Service 递过去，等于把「谁能改全局配置」扩散到每个读点
// （同样的理由见 content 的匿名检索端口、user 的访客账号端口）。
//
// **只读、不做 CRUD**：字典的编辑界面与生命周期是独立批次；现在只做「给下拉供数」
// 这一件事，免得把还没想清楚的编辑语义裹进来。
//
// 实现方与消费方都在 internal：本接口跨模块可见即入契约；页面的选项来源因此不必
// 各自写 SQL（那会破 model 边界）。
type DictReader interface {
	// ListDictOptions 列出某类型（language / currency）的**启用**项。
	ListDictOptions(ctx context.Context, dictType string) ([]DictOption, error)
	// ListCountryOptions 列出启用国家（label 按给定界面语言取中文或英文名）。
	ListCountryOptions(ctx context.Context, lang string) ([]CountryOption, error)
	// CountryLabel 把国家/地区代码换成给定界面语言下的**显示名**（列表 / 详情渲染用）。
	//
	// **查不到一律回落 code**（字典缺行、该语言没有对应名称、code 为空、读取失败）：
	// 它只是展示标签，不是订单语义 —— 少一行字典不该让订单详情整页失败。
	// 这与构建期「国家清单缺失即构建失败」不同：那是产物，这是一次渲染里的一个字段。
	//
	// 与 ListCountryOptions 分工：后者给下拉供数（要顺序、要整份清单），
	// 前者给「已经存了 code、现在要显示」的场景（要查得快，不要每次拉整表）。
	CountryLabel(ctx context.Context, lang, code string) string
}

// === 错误值（跨模块可见的语义错误）===

// ErrGroupNotFound 配置分组不存在。
//
// 为什么是**哨兵错误**而不是让消费方匹配文案：outbound 适配器要把「这一组没有」
// 当成「未配置」处理（退回代码内常量继续跑）。按文案字符串匹配时，文案一改判断就
// 静默失效 —— 表现是把「配置缺失」升级成「站点起不来」。项目里同类判断的既有口径
// 也是哨兵（`errors.Is(err, gorm.ErrRecordNotFound)`）。
//
// enums 里的同名常量是**响应文案**，与这里是语义值与展示文案两层，不是两份真源：
// 哨兵用于进程内判断，文案用于给人看。
var ErrGroupNotFound = errors.New("sysconfig: 配置分组不存在")

// === 索要的端口 ===

// ConfigReader 只读配置口（**窄接口**）：只够 pkg/i18n 这类消费方取「一组配置」。
//
// 为什么单独一条而不是让消费方拿 Service：写能力（SetGroup）与读能力是两条边界 ——
// 进程内的 i18n 全局默认值消费方只需要读，把写方法递过去等于把「谁能改全局配置」
// 从后台页扩散到每一个读点。同样的理由见 content 的匿名检索端口、user 的访客账号端口。
//
// 装配层把它适配成 pkg/i18n 的 ValueLoader（内部 → pkg 的依赖方向，见
// outbound/i18nvalues）。
type ConfigReader interface {
	GetGroup(ctx context.Context, groupKey string) (*Group, error)
}

// === 跨模块形状（本模块 dto 的重导出，不另造一组逐字段等价的定义）===

// Group 一个配置分组。
type Group = sysconfigdto.Group

// SetGroupReq 整组保存请求。
type SetGroupReq = sysconfigdto.SetGroupReq

// DictOption 字典下拉项（language / currency）。
type DictOption = sysconfigdto.DictOption

// CountryOption 国家下拉项。
type CountryOption = sysconfigdto.CountryOption

// SetGroupsReq 多组一次保存请求。
type SetGroupsReq = sysconfigdto.SetGroupsReq

// 分组键：跨模块可见的键名。写死在这里而不是让各方裸写字符串 —— 键写错不报错、
// 只表现为「配置读了但没生效」。
const (
	// GroupI18n 国际化全局默认值组：默认语言 / 站点语言 URL 方案 / 语言码覆盖。
	//
	// 这三项的**唯一来源**就是本组（原先在 config.yaml，改一次要重启且散落多处）；
	// 工程级覆盖仍在 projects.settings（那是多工程维度，不是兼容层）。
	GroupI18n = "i18n"
	// GroupTrade 交易默认值组：默认国家 / 默认货币。
	//
	// **语义是「全局默认 + 兜底」**：工程级覆盖仍走 projects.settings。
	// 目前只有两个读取方，都在展示与结构化数据层（SEO 的 priceCurrency）；
	// 订单与购物车的金额口径仍是固定人民币，多币种是独立特性（见
	// pkg/i18n.RuntimeValues.DefaultCurrency 的注释）。
	GroupTrade = "trade"
	// GroupAI AI 模块的全局开关组。
	//
	// 为什么走 sysconfig 而不是 config.yaml：这里放的是**安全开关**（对外接入点 /
	// 外部工具调用），发现异常流量时第一反应是关掉它 —— 而改 config.yaml 要重启进程，
	// 关窗口的代价不该是一次重启。
	GroupAI = "ai"
)

// ai 组的组内键名。
const (
	// KeyMCPEnabled 是否对外暴露 MCP 接入点（POST /mcp）。
	//
	// **默认关闭**：它是本站数据对外的出口，必须由人显式打开才可达 ——
	// 读不到配置、组不存在、值不是 true，一律按关闭处理（fail closed）。
	// 关闭时端点回 404 而不是 403：探测者不该从响应里知道「这里有个可以打开的东西」。
	KeyMCPEnabled = "mcp_enabled"
)

// i18n 组的组内键名（业务语义键，由消费方解释）。
const (
	// KeyDefaultLang 全局默认语言（完整语言码，如 zh-CN）。
	//
	// 缺失或为空 → 消费方（pkg/i18n）退回代码内常量 fallbackDefaultLang 并记日志，
	// 绝不静默取空值：空默认语言会让产物没有 <html lang>、所有互指都不是 x-default。
	KeyDefaultLang = "default_lang"
	// KeySiteLangURLMode 站点语言 URL 方案：off / default_plain / all_prefix。
	KeySiteLangURLMode = "site_lang_url_mode"
	// KeyLangURLCodes 语言码 → URL 短码的覆盖表（JSON 对象，可选）。
	//
	// 初始不灌数据（内置表 + 主语言子标签回退已覆盖常见情形）；配置了就以它为准，
	// 短码撞车仍在构建期由 LangURLRule.Validate 报错，不静默覆盖。
	KeyLangURLCodes = "lang_url_codes"
)
