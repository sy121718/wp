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
	"sync"
)

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
	// UserID 已认证用户 ID（session 策略时非空）。
	UserID string
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
