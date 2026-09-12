package runtimefragment

// endpoint.go — 片段端点（0-D，docs/04 §1.2）。
// GET/POST /_fragments/{type}：白名单校验 → 认证策略 → 参数限制 → 处理器 → HTML 片段。
// 安全边界：不读 Page Document、不执行 Jet、不接受任意 endpoint（type 必须在
// Registry 内）；参数长度/枚举受限；handler 输出经统一 escape 边界。
//
// issue #20 起支持 POST：结构化入参（表单里的并行数组）用 GET 的 query 传既受长度
// 限制也不合语义。POST 片段分两类：
//
//   · **无副作用**（bundleConfiguratorCheck 是纯计算校验）—— anonymous 即可；
//   · **有副作用**（购物车 / 结算）—— 必须满足下面两条之一，否则就是 CSRF 缺口：
//     ① session 策略（认证态自带 cookie 身份 + CSRF token）；或
//     ② 状态落在**客户端签名 cookie** 里、且该 cookie 是 SameSite=Lax。
//     第 ② 条为什么成立：跨站 POST 在 Lax 下不携带本站 cookie，攻击者构造的请求
//     拿到的是一辆空车 —— 他既无法预置受害者浏览器里的购物车（跨域写不了别家的 cookie），
//     也无法用表单字段伪造一辆车（结算只读 cookie 里的车，不认表单里的商品）。
//     购物车与访客结算走的正是第 ② 条（见 runtimefragment/cart.go 顶部说明）。

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	usercontract "go_wp/internal/module/user/contract"
	"go_wp/pkg/auth"
)

// maxParamLen 查询参数值长度上限（防超长注入）。
const maxParamLen = 200

// maxParamCount GET 的参数名数量上限（query 是扁平结构，几个键就够）。
const maxParamCount = 10

// maxFormParamCount POST 的参数名数量上限：表单用「并行数组」表达结构化入参
// （variantId 出现 N 次 + qty 出现 N 次），捆绑选项上限 20 时约 42 个键。
const maxFormParamCount = 64

// FragmentEndpoint 片段端点 handler。
func FragmentEndpoint(c *gin.Context) {
	typeName := c.Param("type")
	spec, ok := Lookup(typeName)
	if !ok {
		// 未知能力：白名单拒绝（不接受任意 endpoint）。
		c.String(http.StatusNotFound, "片段能力不存在")
		return
	}
	// 方法与能力声明必须一致：GET 能力不接受 POST 调用（反之亦然）。
	if spec.Method != "" && !strings.EqualFold(spec.Method, c.Request.Method) {
		c.String(http.StatusMethodNotAllowed, "片段能力不支持该请求方法")
		return
	}
	// 认证策略。
	userID := ""
	if spec.Auth == AuthSession {
		cs, err := auth.GetCookieSession(c)
		if err != nil || cs == nil {
			c.String(http.StatusUnauthorized, "需要登录")
			return
		}
		userID = strconv.FormatUint(cs.UserID, 10)
	}
	// 访客身份：user 模块的 VisitorIdentityMiddleware 已尽力解析并挂到 context（不阻断）。
	//
	// 未登录时**留空**而不是让端点回 401：HTMX 默认不替换 401 响应的目标节点，
	// 访客会看到一个毫无变化的页面，完全不知道自己需要登录。
	// 所以「必须登录」由具体能力自己声明，并渲染一句引导文案 + 登录链接。
	if userID == "" {
		if v, ok := c.Get(usercontract.VisitorContextKey); ok {
			if id, ok := v.(uint64); ok && id != 0 {
				userID = strconv.FormatUint(id, 10)
			}
		}
	}
	// 参数白名单限制（长度/数量/上下文枚举）。
	params, values, perr := collectFragmentParams(c)
	if perr != nil {
		c.String(http.StatusBadRequest, perr.Error())
		return
	}
	// 槽位解析按**请求内一次**缓存：一个片段请求只服务一个页面（一个工程一种语言），
	// 但同一份渲染里可能问两次（购物车与结算各问一次），第二次不该再查一遍库。
	var slotOnce sync.Once
	var slotCache map[string]string
	req := &Request{
		Type:      typeName,
		Context:   params["context"],
		Params:    params,
		Values:    values,
		UserID:    userID,
		Cookies:   collectFragmentCookies(c),
		IP:        c.ClientIP(),
		UserAgent: strings.TrimSpace(c.GetHeader("User-Agent")),
		SitePagesOf: func(projectID, lang string) map[string]string {
			slotOnce.Do(func() { slotCache = resolveSitePages(c.Request.Context(), projectID, lang) })
			return slotCache
		},
	}
	if err := validateContext(req.Context); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	// 处理器：返回 HTML 片段（handler 内部对用户数据 escape）。
	htmlFragment, err := spec.Render(c.Request.Context(), req)
	if err != nil {
		c.String(http.StatusInternalServerError, "片段渲染失败")
		return
	}
	// 渲染**成功之后**才写 cookie：失败响应配上一个已经更新的 cookie，
	// 会让「页面显示什么」与「服务端记住了什么」各说各话。
	writeFragmentCookies(c, req)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(strings.TrimSpace(htmlFragment)))
}

// collectFragmentCookies 收集请求携带的 cookie（名字 → 值）。
func collectFragmentCookies(c *gin.Context) map[string]string {
	if c == nil || c.Request == nil {
		return nil
	}
	raw := c.Request.Cookies()
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for _, ck := range raw {
		if ck == nil || ck.Name == "" {
			continue
		}
		out[ck.Name] = ck.Value
	}
	return out
}

// writeFragmentCookies 写出处理器声明的响应 cookie。
//
// Secure 按请求协议推断（直连 TLS，或前置代理声明了 X-Forwarded-Proto: https）：
// 访问面的片段端点通常跑在反向代理后面，只看 c.Request.TLS 会把 HTTPS 站点的
// cookie 判成非安全传输，表现是「线上写不进 cookie、本地一切正常」。
func writeFragmentCookies(c *gin.Context, r *Request) {
	if c == nil || r == nil || len(r.SetCookies) == 0 {
		return
	}
	secure := false
	if c.Request != nil {
		secure = c.Request.TLS != nil ||
			strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
	}
	for _, ck := range r.SetCookies {
		if ck.Name == "" {
			continue
		}
		path := ck.Path
		if path == "" {
			path = "/"
		}
		c.SetSameSite(ck.SameSite)
		c.SetCookie(ck.Name, ck.Value, ck.MaxAge, path, "", secure, ck.HTTPOnly)
	}
}

// collectFragmentParams 收集并校验入参：GET 走 query，POST 走表单。
//
// 同时给出「首值」与「全部值」两份视图：前者服务单值参数（productId / context），
// 后者服务并行数组（variantId / qty）—— 只取首值会把多选项静默截成一选项，
// 那正好是最难发现的错误（前台看起来「只算了一件」）。
func collectFragmentParams(c *gin.Context) (params map[string]string, values map[string][]string, err error) {
	params = map[string]string{}
	values = map[string][]string{}
	var source url.Values
	if c.Request.Method == http.MethodPost {
		if perr := c.Request.ParseForm(); perr != nil {
			return nil, nil, errors.New("表单解析失败")
		}
		source = c.Request.PostForm
		if len(source) > maxFormParamCount {
			return nil, nil, errors.New("参数过多")
		}
	} else {
		source = c.Request.URL.Query()
		if len(source) > maxParamCount {
			return nil, nil, errors.New("参数过多")
		}
	}
	for k, vs := range source {
		if len(vs) == 0 {
			continue
		}
		if len(k) > 64 {
			return nil, nil, errors.New("参数非法")
		}
		for _, v := range vs {
			if len(v) > maxParamLen {
				return nil, nil, errors.New("参数非法")
			}
		}
		values[k] = vs
		params[k] = vs[0]
	}
	return params, values, nil
}
