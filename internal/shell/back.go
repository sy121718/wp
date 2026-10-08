package shell

// back.go — 写操作之后的**回跳地址**：由服务端拼，不从表单的隐藏域读整串 URL。
//
// 要替换掉的现状：页面渲染时先用 Go 把「我在哪一页、带什么筛选」算成一个 returnQuery 串
// 塞进隐藏域，POST 回来再解析它、按**每个模块各自一份**的白名单过滤键名、拼出目标 URL
// （order 的 couponBackQuery / couponEchoQuery / couponBackKeys 是样板，三个模块各抄一遍）。
//
// 那套东西存在的原因只有一个：**回跳地址被当成了客户端给的值**。而服务端本来就知道
// 自己在处理哪个工程、哪一页筛选 —— 上下文随表单 action 的 query 一起提交，
// 这里按**调用点显式列出的键**把它读回来即可：白名单从「每个模块自己维护的 map」
// 变成「调用点那一行的参数」，且形状收敛（去空白、长度上限、控制字符）只实现一次。
//
// 两条边界：
//   - 路径必须是站内相对路径（复用 sameOriginPath，与语言切换回跳同一份判据）——
//     否则开放重定向（`//evil.example.com`）就能把管理员送出去；
//   - 只透传调用点列出的键，**值一律不做语义解释**（是不是合法状态码由页面自己按老规矩校验）。
//
// 文案不再经 URL：写操作的结论由提示页在响应体里渲染（见 jump.go）。回跳地址上只带
// 筛选上下文，所以「?err= 是用户可编辑的、要过白名单」那整套读侧判定不再需要。

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
)

// backValueMaxBytes 单个查询值允许的最大字节数。
//
// 200 字节：后台筛选值（工程 id / 状态枚举 / 关键词 / 页码）都远小于它，
// 这个上限挡住的是「让管理员点一个带几 KB query 的链接，回跳时再把它拼回去」。
const backValueMaxBytes = 200

// BackPath 拼写操作后的回跳地址：path + 从**本次请求的 query** 里按 keys 透传的参数。
//
// 典型用法（表单 action 上带上下文，服务端自己读回来）：
//
//	<form method="post" action="/admin/coupons/create?project={{.ProjectID}}&status={{.FilterStatus}}">
//
// 然后 handler 里：
//
//	back := shell.BackPath(c, "/admin/coupons", "project", "status", "keyword", "page", "limit")
//
// path 非法（非站内相对路径 / 超长 / 含控制字符）时记日志并回控制面首页：
// 调用点写的是自己代码里的字面量，非法属于编程错误，但**不能因此把用户打成 500**。
func BackPath(c *gin.Context, path string, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	if !sameOriginPath(path) || len(path) > langRedirectMaxBytes {
		logger.Scene("shell").
			With("path", path).
			Warn("回跳路径不合法（非站内相对路径或超长），回控制面首页")
		return adminHomePath
	}

	q := c.Request.URL.Query()
	params := make(map[string]string, len(keys))
	for _, k := range keys {
		v := strings.TrimSpace(q.Get(k))
		if v == "" {
			continue
		}
		if len(v) > backValueMaxBytes || hasControlRune(v) {
			logger.Scene("shell").
				With("key", k).
				With("len", len(v)).
				Warn("回跳参数被丢弃（超长或含控制字符）")
			continue
		}
		params[k] = v
	}
	return WithParams(path, params)
}

// WithParams 给站内路径追加查询参数：空值丢弃，编码走 url.Values（键有序，产物稳定）。
//
// 单独导出是因为调用点常有一个「不来自 query 的值」要带回去（例如从 PostForm 读到的
// projectId），拼法必须与 BackPath 一致 —— 两处各拼一份就会出现「有的参数被转义、
// 有的没有」这种只在特定值上才暴露的差异。
func WithParams(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		q.Set(k, v)
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
