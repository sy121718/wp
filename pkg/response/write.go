package response

// write.go — 响应出口的**统一写入口**：body（JSON 契约）与头（htmx 读的那条）在一个地方决定。
//
// 为什么单开一个文件：`Success` / `SuccessWithMessage` / `ErrorWithMessage` / `ErrorWithData`
// 四个出口各自 `c.JSON(...)` 时，任何「所有响应都要带的东西」（比如下面这个 message 头）
// 都必须在四处各写一遍 —— 漏一处不会报错，只是那一类响应前端拿不到提示。
//
// 与 body 的关系：**body 完全不变**（{code,message,data} 仍是 AGENTS 写死的契约，
// MCP / AI / fetch 消费者照旧解析）。头只是给 htmx 用的**同一条 message 的副本** ——
// 因为 htmx 的 `hx-swap="none"` 不消费响应体，它只读头。

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// HeaderMessage 响应提示文案的头名。
//
// 为什么需要它（而 body 里已经有 message）：htmx 的写表单用 `hx-swap="none"` ——
// 它不把响应体插进 DOM，于是 body 里的 message 前端根本读不到。想让「服务端取好的词」
// 显示成全局 toast，只有两条路：前端 JSON.parse 响应文本（让前端懂后端格式），
// 或者后端把同一句话再放一份在头里。这里选后者 —— 头是元数据，body 才是数据。
//
// 值的编码协议：**百分号编码，且空格编成 %20 而不是 "+"**（RFC 3986 的
// encodeURIComponent 语义），前端用 decodeURIComponent 精确还原。
//
// 为什么不能用 url.QueryEscape：它属于 application/x-www-form-urlencoded 那一套，
// 把空格编成 "+"；而 decodeURIComponent **不会**把 "+" 还原成空格。实测：
//
//	"Product created successfully" → QueryEscape → "Product+created+successfully"
//	                                → decodeURIComponent → "Product+created+successfully"  ✗
//
// 中文提示里没有空格，所以这个缺陷在中文环境里看不出来 —— 正是审查指出的那类问题。
//
// 也不能让前端「把 + 换回空格」：`+` 本身可能是有效文案字符（"a+b" 经 QueryEscape
// 是 "a%2Bb"，与真正的空格编码 "a+b" 混在一起就分不清了）。所以统一在**编码侧**定协议：
// 空格一律 %20，"+" 一律 %2B，前端只需 decodeURIComponent 一次。
const HeaderMessage = "X-Message"

// writeResponse 所有 JSON 出口的唯一落点。
func writeResponse(c *gin.Context, status int, body Response) {
	// 头先写：c.JSON 会 WriteHeader，之后再加头就无效了。
	if body.Message != "" {
		c.Header(HeaderMessage, encodeHeaderValue(body.Message))
	}
	c.JSON(status, body)
}

// encodeHeaderValue 把文案编成 HTTP 头里可用的 ASCII，且能被 JS 的 decodeURIComponent
// 一次还原（空格 → %20，加号 → %2B）。
//
// 用 url.PathEscape 而不是 QueryEscape：前者属于 path 段那一套，空格编成 %20；
// 但 PathEscape 会**保留** "+"（在 path 里它是合法字符），而我们需要 "+" 也编掉 ——
// 否则前端 decodeURIComponent 看到裸 "+" 会原样保留，与真正的加号无歧义但
// 与「空格被编成 +」的旧行为混淆。所以这里在 PathEscape 之后再把裸 "+" 换成 %2B。
func encodeHeaderValue(s string) string {
	escaped := url.PathEscape(s)
	// PathEscape 保留 "+"，手动编掉以保证「头里没有裸 +」这一条协议。
	return strings.ReplaceAll(escaped, "+", "%2B")
}
