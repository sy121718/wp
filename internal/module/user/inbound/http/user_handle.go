package userhttp

// user_handle.go — 用户模块 HTTP handler 的公共部分。
//
// handler 的职责边界（见 internal/module/CLAUDE.md）：绑定参数 → 调 service → 输出响应。
// 这里**不做业务判断**：邮箱格式、密码强度、账号状态全部在 service 里，
// 这里只负责把表单字段搬进 dto，以及把 service 的错误搬回页面。

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/internal/middleware/builtin"
	userenums "go_wp/internal/module/user/enums"
	userservice "go_wp/internal/module/user/service"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// Handle 用户侧 HTTP handler。
type Handle struct {
	svc *userservice.Service
}

// NewHandle 构造。
func NewHandle(svc *userservice.Service) *Handle { return &Handle{svc: svc} }

// pageTitles 模板名 → 浏览器标题（key + 中文兜底）。
//
// 集中一张表而不是让每个 handler 各自传：这张表同时是「访客侧有哪些页面」的清单，
// 新增页面时若忘了在这里登记，页面标题会退化成站点名 —— 一眼可见，不会静默出错。
//
// 值不是裸中文：<title> 是**直接渲染**的文本，硬写中文等于英文界面里标题永远中文。
var pageTitles = map[string]userLabel{
	"user/login":         {"user.page.login", "登录"},
	"user/register":      {"user.page.register", "注册"},
	"user/register_done": {"user.page.register_done", "注册成功"},
	"user/forgot":        {"user.page.forgot", "找回密码"},
	"user/reset":         {"user.page.reset", "重置密码"},
	"user/message":       {"user.page.message", "提示"},
	"user/account":       {"user.page.account", "账号中心"},
}

// userTextOf 访客页文案的取词入口（等价于 userLabelOf(shell.TranslateFor(c), l)）。
func userTextOf(c *gin.Context, l userLabel) string {
	return userLabelOf(shell.TranslateFor(c), l)
}

// userTextFilled 取词并按命名参数填充 `{name}` 占位符（复用 pkg/i18n 的实现）。
//
// 词条被改坏（填完仍有残留占位符）时 FillTranslate 自动回落中文兜底再填一次；
// 参数缺失也不补默认值：补 0 会渲染出一句「看起来像结论」的错话，
// 而页面显示代码里的原文至少能让人看出「这句没配好」。
func userTextFilled(c *gin.Context, l userLabel, kv map[string]string) string {
	return i18n.FillTranslate(shell.TranslateFor(c), l.key, l.fallback, kv)
}

// render 渲染访客页面，自动补齐 CSRF token 与当前登录用户。
//
// 每个页面都要拿到这两样，逐个 handler 手写必然会漏掉一个 —— 漏掉 CSRF token 的表现是
// 「页面能打开，一提交就 403」，而报错信息指向中间件，排查会绕很远。
func (h *Handle) render(c *gin.Context, status int, name string, data gin.H) {
	if data == nil {
		data = gin.H{}
	}
	if _, ok := data["csrf_token"]; !ok {
		token, err := builtin.EnsureCSRFTokenWith(c, userCSRFStore{})
		if err != nil {
			logger.Scene("user").Error(err, "生成访客 CSRF token 失败")
		}
		data["csrf_token"] = token
	}
	if _, ok := data["user"]; !ok {
		data["user"] = currentSession(c)
	}
	// 界面语言与取词函数同源（response.RequestLanguage 的判定链：语言 Cookie → query lang
	// → Accept-Language → 默认语言）。两者分家会出现「正文按请求语言取词、<html lang>
	// 恒 zh-CN」这类只在部分页面显现的不一致。
	if _, ok := data["lang"]; !ok {
		data["lang"] = response.RequestLanguage(c)
	}
	// i18n 取词函数（与后台 shell.Prepare 注入的 data["t"] 同源）：访客页面模板用
	// {{ .["t"]("user.x", "中文兜底") }} 取词。
	//
	// **必须在这里无条件注入**：chain 索引 + 函数调用在缺 t 时是静默空串
	// （不报错、不 500、无日志），漏掉的后果是整页文案一起变空白
	// （见 internal/templates/CLAUDE.md「缺键的两种后果」）。
	if _, ok := data["t"]; !ok {
		data["t"] = shell.TranslateFor(c)
	}
	// 书写方向（审计 I18N-02）：访客页面与静态产物用同一条规则
	// （builder.DirAttr：RTL 才落字节，LTR 是 HTML 缺省）。判据各写一份就会出现
	// 「静态页是 rtl、账号页不是」这类只在部分页面显现的方向错误。
	if _, ok := data["dir"]; !ok {
		lang, _ := data["lang"].(string)
		data["dir"] = builder.DirAttr(lang)
	}
	if _, ok := data["site"]; !ok {
		data["site"] = "go_wp"
	}
	if _, ok := data["title"]; !ok {
		data["title"] = userLabelOf(shell.TranslateFor(c), pageTitles[name])
	}
	c.HTML(status, name, data)
}

// userFacingText 白名单判定：命中返回原文（**item_key**），未命中返回空串。
//
// 只做判定、不取词。白名单而非黑名单：service 的业务错误全部来自 userenums，
// 而数据库 / Redis 的错误原文可能带表名与 SQL 片段。判定只有这一份，
// 页面出口（userPageMessage / userKeyText）与接口面共用同一张 UserFacingMessages。
func userFacingText(raw string) string {
	msg := raw
	for _, m := range userenums.UserFacingMessages {
		if prefix, _, found := strings.Cut(m, "%s"); found {
			// 带参数的文案（如锁定剩余时间）只比较 %s 之前的部分。
			if strings.HasPrefix(msg, prefix) {
				return msg
			}
			continue
		}
		if msg == m {
			return msg
		}
	}
	return ""
}

// userPageMessage 页面出口的错误文案归口（白名单判定 + **取当前语言的译文**）。
//
// 为什么页面路径必须多这一层取词：userenums 的值是 i18n **item_key**
// （user.err.usernameTaken / user.msg.customerDisabled …），而访客页面
// （user/register、user/account、user/message）里的 {{.error}} / {{.message}} 是
// **直接渲染**的文本，不经过 pkg/response 的 translate —— 只放行 key 的话，
// 访客注册失败看到的是「user.err.usernameTaken」，而不是「用户名已被占用」。
//
// 命中 → 取词；未命中 → 记结构化日志 + 归口文案（原文只进日志）。
// 取词函数据 fallback 原样返回 key：词条缺失时页面显示 key（一眼可见），不静默吞掉整句。
func userPageMessage(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if hit := userFacingText(err.Error()); hit != "" {
		return shell.TranslateFor(c)(hit, hit)
	}
	logger.Scene("user").
		With("path", c.Request.URL.Path).
		Error(err, "用户模块出现未归类错误")
	return shell.TranslateFor(c)(userenums.ErrInternal, "操作失败，请稍后重试")
}

// userKeyText 参数级提示的取词出口：没有 error 对象、文案就是 enums 里的某个常量
// （缺 device 标识、登出失败归口等场景直接传常量给模板）。
//
// 与 userPageMessage 同源（同一份 userFacingText 判定）：白名单内的 key 直接渲染时
// 同样不能裸出。ErrInternal 不在白名单里，会落归口文案 —— 这正是它该有的语义
// （归口文案是「未命中时的返回值」，不是业务文案）。
func userKeyText(c *gin.Context, key string) string {
	if hit := userFacingText(key); hit != "" {
		return shell.TranslateFor(c)(hit, hit)
	}
	return shell.TranslateFor(c)(userenums.ErrInternal, "操作失败，请稍后重试")
}

// formValue 取表单字段并去空白（访客页面的表单字段全部是文本）。
func formValue(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

// formBool 取复选框（HTML 表单未勾选时不提交该字段）。
func formBool(c *gin.Context, key string) bool {
	v := strings.ToLower(strings.TrimSpace(c.PostForm(key)))
	switch v {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// formInt 取整数字段，解析失败按默认值处理。
//
// 静默兜底在别处是坏味道，在这里是刻意的：这些字段（性别 / 每页条数）由页面上的
// 固定选项产生，能收到非法值只可能是手工构造的请求。为它返回一句「参数不合法」
// 不如直接用默认值 —— 真正的取值合法性由 service 兜底（每页条数越界会被拒）。
func formInt(c *gin.Context, key string, def int) int {
	v := strings.TrimSpace(c.PostForm(key))
	if v == "" {
		return def
	}
	n := 0
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return def
		}
		n = n*10 + int(v[i]-'0')
		if n > 1<<30 {
			return def
		}
	}
	return n
}

// clientIP 取访客真实 IP。
//
// 用 c.ClientIP()（gin 依据 TrustedProxies 决定是否采信 X-Forwarded-For）：
// release 模式下 TrustedProxies 为 nil，即不信任任何转发头 —— 这时 c.ClientIP()
// 返回的是直连地址。直接读 XFF 会让任何人都能伪造注册来源 IP。
func clientIP(c *gin.Context) string {
	return strings.TrimSpace(c.ClientIP())
}

// locale 取界面语言（访客页面目前只有 zh-CN，保留参数位是为了邮件模板的语言选择）。
func locale(c *gin.Context) string {
	if v := strings.TrimSpace(c.Query("lang")); v != "" {
		return v
	}
	return "zh-CN"
}

// isNotFound 判断是否为「没找到」类业务错误（用于决定 404 还是 400）。
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == userenums.ErrSessionNotFound || msg == userenums.ErrUserNotFound
}

var _ = errors.New
