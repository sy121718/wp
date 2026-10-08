package shell

// jump.go — 写操作完成后的**整页提示**（对应 ThinkPHP 的 $this->success() / $this->error()）。
//
// 要替换掉的现状：写动作的结论靠 302 + `?err=` / `?ok=` / `?done=` 带回列表页。那条通道
// 的代价是**整套读侧判定**：查询参数不是可信边界（见 notice.go 的文件头），所以每个模块都要
// 维护一份「受控文案 + 数字归一模板」来证明这条提示确实出自本仓 —— order 一个模块就为此
// 长出 orderFacingText / orderDoneTexts / orderBulkTextOf / orderPageDone 与 16 个回跳函数。
//
// 改成由本入口**直接渲染**提示页之后：文案走响应体、不进 URL，不可信输入不在这条链上，
// 读侧判定与回跳通道随之可以整批删除。
//
// 三条边界（不是偏好，各自对应一个已经踩过的坑）：
//   - **内部错误仍走 PageError**：这里只呈现**已经被判定可以给用户看**的文案（模块先过自己的
//     白名单），原文只进日志 —— 换个页面呈现不等于可以把 err.Error() 铺在页面上；
//   - **htmx 请求走 HX-Redirect**（与既有 couponRedirectWhere 的分档一致）：htmx 会把响应
//     换进目标节点，整页 HTML 塞进去是坏页面。代价是 htmx 那一档**看不到提示文案** ——
//     与现状一致，不引入回退；要保住文案需要一套闪存（session）机制，不在本批；
//   - **回跳地址必须站内**：复用 sameOriginPath（与语言切换回跳同一份判据），
//     否则一个提示页就成了开放重定向的跳板。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Jump 是一次提示页的全部输入。
type Jump struct {
	// OK 决定外观（成功 / 失败）。
	OK bool
	// Msg 提示正文。空值回落归口文案 —— 页面不能显示空串。
	//
	// 文案由调用方给：**模块先过自己的错误白名单**（可透出的业务文案），
	// 本入口不猜「这是不是业务错误」—— 猜错的代价是「有时泄露有时吞掉」，最难被发现。
	Msg string
	// Back 自动跳转目标：**站内相对路径**。非法值丢弃并回控制面首页。
	Back string
	// BackText 「立即前往」的链接文字；空则不渲染链接（仍会自动跳转）。
	//
	// 刻意不给默认文案：默认值要么新增一条全站词条（本次不引入），要么借别的模块的词条
	// （跨模块借词条会随对方改动漂移）。调用点本来就有一句现成的话（「返回优惠码列表」）。
	BackText string
	// Seconds 停留秒数：>0 时按 meta refresh 自动跳转；<=0 只给链接，不自动跳。
	Seconds int
}

// jumpView 提示页的渲染数据（struct：模板字段名由编译器保证，不再靠约定）。
type jumpView struct {
	OK       bool
	Msg      string
	Back     string
	BackText string
	Seconds  int
}

// RenderJump 渲染整页提示（HTTP 200）。
func RenderJump(c *gin.Context, j Jump) {
	if c == nil {
		return
	}

	back := strings.TrimSpace(j.Back)
	if !sameOriginPath(back) {
		back = adminHomePath
	}

	// htmx：换 HX-Redirect 整页跳转，不把整页 HTML 塞进片段位置。
	if IsHXRequest(c) {
		c.Header("HX-Redirect", back)
		c.Status(http.StatusOK)
		return
	}

	msg := strings.TrimSpace(j.Msg)
	if msg == "" {
		msg = PageInternalText(c)
	}
	seconds := j.Seconds
	if seconds < 0 {
		seconds = 0
	}

	c.Header("Cache-Control", "no-store")
	// 标题用提示正文本身：既不新增全站词条，又让标签页说清刚才发生了什么。
	// Prepare 会把 title 当 key 再查一次词条（查不到回落原文），所以这里传成品文案是安全的。
	c.HTML(http.StatusOK, "admin/jump.html", Prepare(c, gin.H{
		"title": msg,
		"Jump": jumpView{
			OK:       j.OK,
			Msg:      msg,
			Back:     back,
			BackText: strings.TrimSpace(j.BackText),
			Seconds:  seconds,
		},
	}))
}

// IsHXRequest 是否为 htmx 发起的请求。
//
// 导出而不是各模块各写一份：这段判定原本在 order（couponIsHXRequest）、product、inventory
// 各有一份私有实现，判据是同一个请求头 —— 分档规则（失败重渲片段 / 成功 HX-Redirect）
// 依赖它，抄错一处就会出现「某个模块的失败分档静默失效」。
func IsHXRequest(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}
