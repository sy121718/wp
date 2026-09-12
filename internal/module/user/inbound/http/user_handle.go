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

	"go_wp/internal/middleware/builtin"
	userenums "go_wp/internal/module/user/enums"
	userservice "go_wp/internal/module/user/service"
	"go_wp/pkg/logger"
)

// Handle 用户侧 HTTP handler。
type Handle struct {
	svc *userservice.Service
}

// NewHandle 构造。
func NewHandle(svc *userservice.Service) *Handle { return &Handle{svc: svc} }

// pageTitles 模板名 → 浏览器标题。
//
// 集中一张表而不是让每个 handler 各自传：这张表同时是「访客侧有哪些页面」的清单，
// 新增页面时若忘了在这里登记，页面标题会退化成站点名 —— 一眼可见，不会静默出错。
var pageTitles = map[string]string{
	"user/login":         "登录",
	"user/register":      "注册",
	"user/register_done": "注册成功",
	"user/forgot":        "找回密码",
	"user/reset":         "重置密码",
	"user/message":       "提示",
	"user/account":       "账号中心",
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
	if _, ok := data["lang"]; !ok {
		data["lang"] = "zh-CN"
	}
	if _, ok := data["site"]; !ok {
		data["site"] = "go_wp"
	}
	if _, ok := data["title"]; !ok {
		data["title"] = pageTitles[name]
	}
	c.HTML(status, name, data)
}

// userMessage 把 service 返回的错误转成可以展示给访客的文案。
//
// 白名单而非黑名单：service 的业务错误全部来自 userenums（面向用户的中文），
// 而数据库 / Redis 的错误原文可能带表名与 SQL 片段。默认落到通用文案，
// 顺带记一条日志 —— 否则「页面上什么都没说」会变成最难查的一类问题。
func userMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
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
	logger.Scene("user").Error(err, "用户模块出现未归类错误")
	return userenums.ErrInternal
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
