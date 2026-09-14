// Package runtimefragment 运行时片段能力注册表（0-D，docs/04 §1.2）。
//
// Fragment Registry 是「静态站 + 动态能力」的桥：capability 白名单（type →
// 处理器 + 匿名/认证策略）。Page Document 只保存语义化 Fragment Type，
// 不保存 endpoint/脚本；Handler 不读 Page Document、不执行 Jet、不解释
// Binding——只实现该 capability 的固定运行时协议并返回 HTML 片段。
package runtimefragment

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/pkg/logger"
)

// sitePageResolver 系统页面槽位解析器（装配期注入一次）。
//
// 与 cartProvider 同模式：片段层只依赖 page 模块的**受限读接口**（SitePageResolver），
// 拿不到发布 / 删除 / 改 URL 的能力 —— 越权防护靠接口形状。
var sitePageResolver pagecontract.SitePageResolver

// SetSitePageResolver 注入系统页面槽位解析器（装配期调用；未注入时槽位一律为空）。
func SetSitePageResolver(r pagecontract.SitePageResolver) { sitePageResolver = r }

// resolveSitePages 解析「槽位 → 当前语言线上路径」。
//
// 失败时返回 nil 并记日志、**不让片段渲染失败**：槽位决定的是「链接能不能点」，
// 不是片段能否渲染的前提 —— 为它把整个购物车片段打成 500，损失远大于收益。
func resolveSitePages(ctx context.Context, projectID, lang string) map[string]string {
	if sitePageResolver == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	pages, err := sitePageResolver.ResolveSitePages(ctx, projectID, lang)
	if err != nil {
		logger.Scene("fragment").Error(err, "系统页面槽位解析失败")
		return nil
	}
	return pages
}

// Request 单次片段请求（props 已由 endpoint 白名单校验）。
type Request struct {
	// Type 能力类型（白名单 key）。
	Type string
	// Context 语义上下文（currentProduct / visitorSession / searchQuery，可选）。
	Context string
	// Params 受限查询参数（已长度/枚举校验；多值参数只保留首值）。
	Params map[string]string
	// Values 全部参数值（并行数组用：POST 表单里同名多值，如 variantId / qty）。
	// 与 Params 同源同校验，只是不做「取首值」的折叠。
	Values map[string][]string
	// CSRFToken 访客域的 CSRF token（片段渲染表单用）。
	//
	// 静态产物烘不进它（token 在签名会话 cookie 里），所以带表单的片段必须现读现写：
	// 少了它，片段渲染出的表单提交永远 403。
	CSRFToken string
	// UserID 已认证身份 id（十进制字符串）。
	//
	// 两个来源，按优先级取：① session 策略命中的**后台账号** id；
	// ② 访客身份中间件解出的**访客账号** id（未登录时为空）。
	// 顺序不能反：后台会话与访客会话是两套 cookie、两个身份域，
	// 同一次请求里同时存在是可能的（管理员在前台逛自己的站），
	// 而后台身份是更强的那个声明。
	UserID string
	// VisitorToken 访客会话令牌（原值）。
	//
	// 为什么片段层需要它：登录设备台账（user_sessions）存的是令牌的 **sha256**，
	// 要判断「列表里哪一台是我现在用的」就得拿原值去比对。
	//
	// **它不是可展示数据**：令牌能换一个已登录会话。唯一的合法用途是作为
	// VisitorAccountPort.SessionsOf 的入参；任何处理器把它写进输出都是缺陷
	// （片段层的对策是「只在一处消费」，而不是靠每个处理器自觉）。
	// 未登录时为空串。
	VisitorToken string
	// Cookies 本次请求携带的 cookie（原样，未解析）。
	//
	// 它替代的是「处理器直接摸 *gin.Context」：购物车与归因采集都把状态放在客户端
	// 签名 cookie 里，处理器必须读得到，但读 cookie 这件事本身不该带来操作响应的能力。
	// go 标准库的 Request.Cookies() 已经带数量与长度防护，不需要在这里再包一层。
	Cookies map[string]string
	// IP 客户端地址（按部署的 TrustedProxies 配置解析）。
	//
	// 订单表有这一列，归因里也用它判「同一访客」；但要清楚它是**尽力而为**的：
	// 反向代理配置、NAT、移动网络都会让它失真。用于分析可以，用于身份判定不行。
	IP string
	// UserAgent 请求头里的 UA（服务端看到的那个，不是脚本自报的）。
	UserAgent string
	// SetCookies 处理器要求写入响应的 cookie（由 endpoint 在渲染成功后统一写出）。
	//
	// 为什么不把 *gin.Context 交给处理器：处理器是「请求 → HTML」的白名单注册体，
	// 让它直接操作响应会让「片段能不能改响应头」变成每个处理器各自为政的问题。
	// 收成一个声明式字段之后，需要审核的地方就只有 endpoint 里那一小段。
	//
	// 渲染失败时这些 cookie **不会**被写出：一个报错的响应配上「购物车已更新」的
	// cookie，会让前端与服务端各说各话。
	SetCookies []ResponseCookie
	// Lang 本次片段请求语言（endpoint 解析 ?lang= 并按 project_locales 校验后写入，I18N-011）。
	Lang string
	// T 构建期冻结的 sys_i18n 取词函数（签名 func(key, fallback string) string）。
	T func(key, fallback string) string
	// SitePagesOf 解析「系统页面槽位 → 当前语言线上路径」（BIZ-1）。
	//
	// 做成**函数**而不是预填的 map：多数片段（库存、商品列表）不需要槽位，
	// 每次片段请求都为它们查两张表是白花钱。需要的那几个（购物车、结算）自己调，
	// 不调就零成本。
	//
	// 未接入解析器时返回空表，片段据此不输出链接 —— 这是正常状态（站还没配），
	// 不是错误；**绝不猜路径**，猜错的链接比没有链接难查得多。
	SitePagesOf func(projectID, lang string) map[string]string
}

// ResponseCookie 片段处理器要写到响应上的 cookie。
//
// 刻意不暴露 Domain / Expires：跨子域共享与绝对过期时间都是**部署策略**，
// 不该由某一个能力自己决定。
type ResponseCookie struct {
	Name  string
	Value string
	// MaxAge 秒；0 表示会话 cookie（不写 Max-Age）。
	MaxAge int
	// HTTPOnly 是否禁止脚本读取。购物车 cookie 必须是 true ——
	// 没有脚本需要读它，而放开了就等于把「买了什么」暴露给任何一段注入脚本。
	HTTPOnly bool
	// SameSite 同站策略；请显式给值，零值等同于浏览器默认行为。
	//
	// 匿名写能力的 CSRF 防线就在这里：SameSite=Lax 让跨站 POST **不携带** cookie，
	// 攻击者构造的请求拿到的是一辆空车，只会得到一个「购物车是空的」。
	SameSite http.SameSite
	// Path 缺省为 "/"。
	Path string
}

// Spec 一个运行时片段能力（capability 白名单条目）。
type Spec struct {
	// Type 能力类型（如 loginPanel / cartSummary）。
	Type string
	// Method HTTP method（GET 无副作用 / POST 写操作带 CSRF）。
	Method string
	// Auth 认证策略：anonymous（公开）/ session（需登录）。
	Auth string
	// Render 处理器：返回 HTML 片段（由 Registry 统一 escape/sanitize 边界，
	// 处理器内部输出的用户数据必须经 html.EscapeString）。
	Render func(ctx context.Context, r *Request) (string, error)
}

// 认证策略常量。
const (
	AuthAnonymous = "anonymous"
	AuthSession   = "session"
)

// registry 能力白名单（进程级，包 init 自注册内置 capability）。
var (
	registryMu sync.RWMutex
	registry   = map[string]Spec{}
)

// Register 注册片段能力（重复类型覆盖，便于测试替换）。
func Register(s Spec) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[s.Type] = s
}

// Lookup 按类型查能力（白名单校验的唯一入口）。
func Lookup(typeName string) (s Spec, ok bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	s, ok = registry[typeName]
	return s, ok
}

// Types 全部能力类型（字典序，确定性）。
func Types() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// validateContext 语义上下文白名单（协议：context 只能是枚举值）。
func validateContext(ctx string) error {
	switch ctx {
	case "", "currentProduct", "visitorSession", "searchQuery":
		return nil
	}
	return fmt.Errorf("非法的片段上下文: %q", ctx)
}
