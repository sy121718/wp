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

	"github.com/gin-gonic/gin"
)

// HeaderMessage 响应提示文案的头名。
//
// 为什么需要它（而 body 里已经有 message）：htmx 的写表单用 `hx-swap="none"` ——
// 它不把响应体插进 DOM，于是 body 里的 message 前端根本读不到。想让「服务端取好的词」
// 显示成全局 toast，只有两条路：前端 JSON.parse 响应文本（让前端懂后端格式），
// 或者后端把同一句话再放一份在头里。这里选后者 —— 头是元数据，body 才是数据。
//
// 值经 url.QueryEscape 编码：HTTP 头只能是 ASCII，中文文案直接写会破坏响应头
// （Go 的 net/http 会拒绝，或经代理时静默丢）。前端 decodeURIComponent 还原。
const HeaderMessage = "X-Message"

// writeResponse 所有 JSON 出口的唯一落点。
func writeResponse(c *gin.Context, status int, body Response) {
	// 头先写：c.JSON 会 WriteHeader，之后再加头就无效了。
	if body.Message != "" {
		c.Header(HeaderMessage, url.QueryEscape(body.Message))
	}
	c.JSON(status, body)
}
