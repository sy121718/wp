package aihttp

// ai_fab_stream.go — 悬浮球的流式回答端点（SSE）。
//
// 与非流式的 /admin/ai/ask 的关系：**同一个 defaultModel、同一个 composeFabInput、
// 同一个会话**，差别只在「怎么把过程交给浏览器」。刻意不复制这两段逻辑：
// 它们决定了「模型看到的当前页面是什么」，复制一份必然在下次改页面上下文时漏掉一边，
// 而漏掉的症状是「用流式时模型不知道你在哪个页面」。
//
// 为什么用 SSE 而不是 WebSocket：这是单向的服务器推送，SSE 是它的原生形态 ——
// 浏览器自动重连、事件分隔符由协议定义、HTTP/2 下还能多路复用。WebSocket 要
// 自己定义分帧与心跳，换不来任何这里需要的东西。

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/pkg/response"
)

// fabStreamChunk 推给浏览器的一条 SSE 数据。
//
// 字段名刻意短（每次增量都要序列化一遍）：一次回答可能有几百条。
type fabStreamChunk struct {
	// Kind 取值：reasoning | text | tool | done | error。
	//
	// 与 aiservice.StreamEvent 的 Kind 一一对应，另加两个只有传输层才有的：
	// done（带最终结果）与 error（把失败说成人话）。这样浏览器只需认识一套词。
	Kind string `json:"kind"`
	// Text 本条的文本。
	Text string `json:"text,omitempty"`
	// Answer 只在 done 时给：模型这一轮的完整正文（浏览器用它替换掉逐字拼出来的那份，
	// 两者**不应该有差别** —— 有差别说明增量累积漏了或多了，那本身就是缺陷信号）。
	Answer string `json:"answer,omitempty"`
	// Note 只在 done 时给：截断提示、查了几次数据这类附注（整句由服务端拼好）。
	Note string `json:"note,omitempty"`
}

// FabAskStream POST /admin/ai/ask/stream → SSE 流式回答。
//
// 状态码一律 200（连同错误）：SSE 的客户端把非 2xx 当成传输失败，拿不到我们
// 精心写的错误文案；把失败做成一条 kind=error 的事件，浏览器才能把它显示在
// 面板里（与会话页 / 悬浮球非流式路径同一条口径）。
func (h *SessionPageHandle) FabAskStream(c *gin.Context) {
	providerKey, model, ok := h.defaultModel(c.Request.Context())
	if !ok {
		writeFabStreamError(c, fabText(aienums.MsgFabNoModel))
		return
	}
	input := strings.TrimSpace(c.PostForm("input"))
	if input == "" {
		writeFabStreamError(c, fabText(aienums.MsgFabEmptyInput))
		return
	}
	truncated := false
	if runes := []rune(input); len(runes) > fabInputLimit {
		input = string(runes[:fabInputLimit])
		truncated = true
	}

	// 先写头再写第一个字节：不预热的话 gin 会等第一次 Write 才发头，
	// 而那之前的几十毫秒浏览器拿不到任何响应（表现为「点了没反应」）。
	// X-Accel-Buffering: no 关掉反向代理的响应缓冲 —— 开着的话整个流会被
	// 代理攒到最后一次性下发，逐字效果完全消失（而这在本地直连时看不出来）。
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	flush := func(chunk fabStreamChunk) {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		if _, werr := c.Writer.Write([]byte("data: " + string(payload) + "\n\n")); werr != nil {
			// 浏览器关掉了面板 / 断开了：写不进去只意味着没人听了，
			// 不是错误。中断由下面的 ctx 取消传导到上游。
			return
		}
		c.Writer.Flush()
	}

	res, err := h.svc.SendMessageStream(c.Request.Context(), aidto.SendMessageReq{
		SessionKey:  fabSessionKey,
		ProviderKey: providerKey,
		Model:       model,
		Input:       composeFabInput(c, input),
		UserText:    input,
		UserID:      userID(c),
	}, func(ev aiservice.StreamEvent) {
		flush(fabStreamChunk{Kind: ev.Kind, Text: ev.Text})
	})
	if err != nil {
		flush(fabStreamChunk{Kind: "error", Text: fabText(aienums.MsgFabFailed)})
		return
	}

	answer := strings.TrimSpace(res.AssistantEvent.Content)
	note := fabDoneNote(res, truncated)
	flush(fabStreamChunk{Kind: "done", Answer: answer, Note: note})
}

// fabDoneNote 拼收尾那句附注（没有可说的就回空串）。
func fabDoneNote(res *aidto.SendMessageResult, truncated bool) string {
	parts := make([]string, 0, 2)
	if truncated {
		parts = append(parts, fabText(aienums.MsgFabTruncated))
	}
	if n := len(res.ToolEvents); n > 0 {
		parts = append(parts, fabText(aienums.MsgFabToolsPrefix)+strconv.Itoa(n)+fabText(aienums.MsgFabToolsSuffix))
	}
	return strings.Join(parts, " ")
}

// FabHistory GET /admin/ai/fab/history：把这条会话已有的对话交给界面。
//
// 存在的理由：悬浮球与概览页的提问框都是单次问答的渲染形态 —— 关掉面板、刷新页面、
// 切到另一个后台页，界面上就只剩一个空输入框，看起来像搜索引擎；而服务端一直是
// 同一条会话在续写（fabSessionKey 固定，历史进 stablePrefix）。模型记得上一轮，
// 用户却看不见，于是「它是个搜索框」这个印象来自界面而不是能力。
//
// 只读、不建会话：首访（还没问过任何问题）回空数组而不是 404。
func (h *SessionPageHandle) FabHistory(c *gin.Context) {
	turns, err := h.svc.RecentDialogue(c.Request.Context(), fabSessionKey, 0)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	if turns == nil {
		turns = []aidto.DialogueTurn{}
	}
	response.Success(c, gin.H{"items": turns})
}

// writeFabStreamError 以一条 error 事件结束（状态码仍是 200，理由见 FabAskStream）。
func writeFabStreamError(c *gin.Context, text string) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(fabStreamChunk{Kind: "error", Text: text})
	_, _ = c.Writer.Write([]byte("data: " + string(payload) + "\n\n"))
	c.Writer.Flush()
}
