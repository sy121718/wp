// ai_chat_handle.go — 对话入口的 JSON handler（参数绑定 → service → 统一响应）。
//
// 只做绑定与响应：文案一律取 enums，业务判断在 service。参数可走 body（json）或 Query（form），
// 与 ai_handle.go 的其它接口同口径（路由只用 GET / POST）。
package aihttp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/pkg/response"
)

// Chat POST /api/ai/chat → 一次对话，回文本。
//
// 绑定失败归口 ErrInvalidParam；供应商不存在归 404，其余业务错误归 400。
// 上游报文的原文不出现在响应里（service 侧只回 enums key，原文进日志）。
func (h *Handle) Chat(c *gin.Context) {
	var req aidto.ChatReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	// 归属由服务端定，**不采信请求体**：调用流水里的「谁调用的」如果来自请求参数，
	// 任何人都能把别人的名字写进审计流水。SessionID 一律清零 —— 这条路由不属于任何会话
	// （会话页的发消息走 SendMessage，那边由会话层填自己正在续写的那条）。
	req.UserID = userID(c)
	req.SessionID = 0
	result, err := h.svc.Chat(c.Request.Context(), &req)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, aiservice.ErrProviderNotFound) {
			code = http.StatusNotFound
		}
		response.ErrorAuto(c, code, "ai", err)
		return
	}
	response.Success(c, result)
}
