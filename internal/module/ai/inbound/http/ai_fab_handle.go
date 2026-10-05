package aihttp

// ai_fab_handle.go — 全局 AI 悬浮球的一次问答（docs/17 §1.1 第 3 项、D9）。
//
// 与会话页的 SessionSend 差在三处，值得写清楚：
//  1. **不选模型**：悬浮球在任意页面上被点开，让用户先选供应商等于把「随手问一句」
//     变成一次配置操作；这里用「第一个可用的供应商 + 它的第一个模型」。
//  2. **返回片段而不是重定向**：回答要出现在球旁边，而不是把用户弹到另一个页面
//     （弹走之后他回来就丢了原来的上下文）。
//  3. **带上页面上下文**（D9）：path + query + 页面标题。**不注入页面语义** ——
//     40+ 页面逐个登记「这个页面在筛什么」就是第二份真源，注定与页面本身分叉；
//     注入位置信息、让模型自己说它的理解，错了也看得见（规则里已要求它明说）。
//
// 会话用固定的会话键（每个后台账号一条），所以悬浮球里的对话是连续的，
// 同时它也会出现在会话页的列表里 —— 不另开一套只在这里可见的历史。

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/internal/uispec"
)

// fabSessionKey 悬浮球的会话键（EnsureSession 按 userID 隔离，所以每个人一条）。
const fabSessionKey = "admin-fab"

// fabInputLimit 一次提问的字符上限。
//
// 不给上限时一次粘贴可以把整个请求顶爆，而失败发生在出站层 —— 用户看到的是
// 「模型调用失败」，与「你贴太多了」完全对不上。截断而不是拒绝：悬浮球是随手用的，
// 报错让人重来一遍比截断更烦，而截断是可见的（下面会附一句说明）。
const fabInputLimit = 4000

// fabContextQueryLimit 页面 query 串的截断长度。
//
// 它可能很长（筛选条件多的列表页），而它对回答的用处只在「这个页面正按什么筛」，
// 所以留前若干字符足够；超出部分截掉并在注入文本里说明。
const fabContextQueryLimit = 300

// FabAsk POST /admin/ai/ask：悬浮球发一条消息，回一段可渲染的片段。
func (h *SessionPageHandle) FabAsk(c *gin.Context) {
	input := strings.TrimSpace(c.PostForm("input"))
	providerKey, model, ok := h.defaultModel(c.Request.Context())
	if !ok {
		// 没有可用模型时**直说**，不要让请求走到出站层再失败：
		// 那里的错误是「模型调用失败」，用户会去查网络，而真正的原因是没配供应商。
		h.renderFab(c, gin.H{
			"FabError":  fabText(aienums.MsgFabNoModel),
			"FabAskKey": c.PostForm("ctxPath"),
		})
		return
	}
	if input == "" {
		h.renderFab(c, gin.H{
			"FabError":  fabText(aienums.MsgFabEmptyInput),
			"FabAskKey": c.PostForm("ctxPath"),
		})
		return
	}
	truncated := false
	if runes := []rune(input); len(runes) > fabInputLimit {
		// 按 rune 截断：按字节截会把一个汉字切成两半，那半个字会一路进数据库与请求体。
		input = string(runes[:fabInputLimit])
		truncated = true
	}

	res, err := h.svc.SendMessage(c.Request.Context(), aidto.SendMessageReq{
		SessionKey:  fabSessionKey,
		ProviderKey: providerKey,
		Model:       model,
		Input:       composeFabInput(c, input),
		UserID:      userID(c),
	})
	if err != nil {
		h.renderFab(c, gin.H{
			"FabError":  fabText(aienums.MsgFabFailed),
			"FabAskKey": c.PostForm("ctxPath"),
		})
		return
	}
	answer := strings.TrimSpace(res.AssistantEvent.Content)
	views := fabViewsOf(res)
	data := gin.H{
		"FabAnswer":    answer,
		"FabViews":     views,
		"FabSessionID": res.Session.ID,
		"FabAskKey":    c.PostForm("ctxPath"),
	}
	if len(views) > 0 {
		// 与 AI 会话页同一形态：ui_blocks.html 自己 `{{range .Views}}`，
		// 所以 include 时**必须**传一个带 Views 键的上下文，不能把切片本身传进去。
		// Jet 的 include 只接受变量表达式，不接受 `gin.H{...}` 字面量（解析期就报
		// `unexpected token '{'`），所以这个包装必须在 Go 侧做好。
		data["FabViewsCtx"] = gin.H{"Views": views}
	}
	if truncated {
		data["FabTruncatedText"] = fabText(aienums.MsgFabTruncated)
	}
	if n := len(res.ToolEvents); n > 0 {
		data["FabToolsText"] = fabText(aienums.MsgFabToolsPrefix) + strconv.Itoa(n) + fabText(aienums.MsgFabToolsSuffix)
	}
	// 既没有话、也没有取过数：可能是模型只调了工具但工具全失败。
	// 这句要说清「可以去看详情」—— 详情里有工具名与参数，那才是排查的入口。
	if answer == "" && len(res.ToolEvents) == 0 {
		data["FabEmptyText"] = fabText(aienums.MsgFabEmpty)
	}
	h.renderFab(c, data)
}

// renderFab 渲染悬浮球的结果片段。
//
// **一律 200**：面板里的每一句话（没写问题、没配模型、这轮失败）都是**正常的业务回答**，
// 而 htmx 默认不替换 4xx/5xx 的响应 —— 用状态码表达业务结果会让用户点了按钮
// 看到面板毫无变化。真正的内部错误（模板渲染失败）由框架自己回 500，那才是 5xx 的位置。
func (h *SessionPageHandle) renderFab(c *gin.Context, data gin.H) {
	c.HTML(http.StatusOK, "partials/ai_fab_result", data)
}

// fabViewsOf 从本轮的工具事件里收集可渲染视图（P5 的渲染数据通道）。
//
// 取的是**工具事件**而不是助手事件：视图是取数工具挂上去的（ai_session_render.go 的
// 逐块取数），助手事件只有文本。多个工具各出几张视图时按发生顺序拼起来，
// 与它们在会话页时间线里的顺序一致 —— 两处顺序不同会让人怀疑自己看的是不是同一次回答。
func fabViewsOf(res *aidto.SendMessageResult) []uispec.View {
	if res == nil {
		return nil
	}
	out := make([]uispec.View, 0, len(res.ToolEvents))
	for _, ev := range res.ToolEvents {
		out = append(out, renderViewsOf(ev.Meta)...)
	}
	return out
}

// defaultModel 取一个可用于悬浮球的供应商与模型。
//
// 判据三条，缺一不可：**启用**（停用的供应商不该被自动选中）、**有密钥**（没有密钥
// 调不通，而失败信息会指向出站层）、**模型目录非空**（目录空时选不出模型名）。
// 取第一个满足的而不是「最近更新的」：这里要的是**稳定**（每次点开用同一个），
// 而不是「最优」—— 选谁由用户在会话页决定，悬浮球只求能跑。
func (h *SessionPageHandle) defaultModel(ctx context.Context) (providerKey, model string, ok bool) {
	if h.providers == nil {
		return "", "", false
	}
	list, err := h.providers.ListProviders(ctx)
	if err != nil {
		return "", "", false
	}
	for _, p := range list {
		if p.Status != aiProviderEnabled || !p.HasAPIKey || len(p.Models) == 0 {
			continue
		}
		key := strings.TrimSpace(p.ProviderKey)
		id := strings.TrimSpace(p.Models[0].ID)
		if key == "" || id == "" {
			continue
		}
		return key, id, true
	}
	return "", "", false
}

// aiProviderEnabled 供应商「启用」的落库值（与配置面同口径）。
const aiProviderEnabled = 1

// composeFabInput 把页面上下文与本轮问题拼成一段输入。
//
// **格式固定、逐行标注**：模型要靠这几行判断「用户是在某个列表页问的」。
// 上下文缺失时整段不写（不写「（无）」占位）—— 占位会让模型以为自己在一个空页面上。
//
// 只用 path + query + 标题，不注入 DOM、不注入页面正文：页面正文里可能有别人的数据，
// 而 path 与 query 是浏览器地址栏里本来就可见的东西，注入它们不扩大任何信息面。
func composeFabInput(c *gin.Context, input string) string {
	var b strings.Builder
	path := strings.TrimSpace(c.PostForm("ctxPath"))
	query := strings.TrimSpace(c.PostForm("ctxQuery"))
	title := strings.TrimSpace(c.PostForm("ctxTitle"))
	if path != "" || query != "" || title != "" {
		b.WriteString("（当前页面：")
		if title != "" {
			b.WriteString(title)
		}
		if path != "" {
			if title != "" {
				b.WriteString(" ")
			}
			b.WriteString(path)
		}
		if query != "" {
			q := query
			if len([]rune(q)) > fabContextQueryLimit {
				q = string([]rune(q)[:fabContextQueryLimit]) + "…（已截断）"
			}
			b.WriteString(q)
		}
		b.WriteString("）\n")
	}
	b.WriteString(input)
	return b.String()
}

// fabText 把 key 换成面向用户的文案。
//
// **必须走这一步**：`aienums.MsgFab*` 是 **key**（`ai.fab.tools` 这种），
// 文案在 `FacingMessages` 里。直接把常量塞进模板的结果是把 key 原样渲染给用户 ——
// 实测真跑一次时页面上出现 `ai.fab.tools2ai.fab.toolsUnit`，而那轮恰好是
// 「模型要求补上下文」的正常回答，看起来像模板坏了（这个缺陷只有真调模型才会暴露，
// 假上游的用例断言的是「渲染出了那句提示」，不会去看那句提示长什么样）。
//
// 取不到时回 key 本身（可见的降级）：这一层是异常路径的最后兜底，
// 宁可显示一个 `ai.fab.xxx` 也不要显示空白 —— 空白会被当成「按钮没生效」。
func fabText(key string) string {
	if v, ok := aienums.FacingText(key); ok {
		return v
	}
	return key
}
