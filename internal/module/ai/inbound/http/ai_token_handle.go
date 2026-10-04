// ai_token_handle.go — 对外访问令牌（PAT）的 JSON 接口。
//
// 三个入口，一码一路由（与同模块其它接口同口径）：
//
//	GET  /api/ai/token/list   列表（all=1 看全站）
//	POST /api/ai/token/create 签发（**响应里带一次性明文**）
//	POST /api/ai/token/revoke 撤销（不删行）
//
// 明文只出现在 create 的响应里：列表接口永远不回明文与哈希，
// 因为列表要渲染进后台页面，而「页面能看到的」等于「能被截屏带走的」。
package aihttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/pkg/response"
)

// TokenHandle 令牌接口的处理器。
type TokenHandle struct {
	svc *aiservice.AccessTokenService
}

// NewTokenHandle 构造处理器；svc 为 nil 时各接口回 500（装配缺陷要看得见）。
func NewTokenHandle(svc *aiservice.AccessTokenService) *TokenHandle { return &TokenHandle{svc: svc} }

// tokenCreateBody 签发请求体。归属账号**不在**请求体里（服务端从登录态取）。
type tokenCreateBody struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expiresAt"`
}

// tokenRevokeBody 撤销请求体。
type tokenRevokeBody struct {
	ID int64 `json:"id"`
}

// List GET /api/ai/token/list?all=&limit= → 令牌列表。
func (h *TokenHandle) List(c *gin.Context) {
	if h.svc == nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, aienums.ErrInternal)
		return
	}
	// all 的权限判定在 service（它决定查全站还是只查自己）：页面路由与 API 用的是同一个权限点，
	// 但「能看全站」这件事需要 AITokenList 之外的判断时，改动点只有 service 一处。
	req := &aidto.TokenListReq{
		All:    c.Query("all") == "1" || c.Query("all") == "true",
		Limit:  int(parseInt64(c.Query("limit"))),
		UserID: userID(c),
	}
	rows, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"list": rows})
}

// Create POST /api/ai/token/create → 签发一把令牌（响应里的 token 是**唯一一次**明文）。
func (h *TokenHandle) Create(c *gin.Context) {
	if h.svc == nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, aienums.ErrInternal)
		return
	}
	var body tokenCreateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &aidto.TokenCreateReq{
		Name:      body.Name,
		Scopes:    body.Scopes,
		ExpiresAt: body.ExpiresAt,
		// 归属账号由服务端定，不采信请求体（否则令牌可以挂到别人名下，审计失去意义）。
		UserID: userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, res)
}

// Revoke POST /api/ai/token/revoke → 撤销一把令牌（不删行）。
func (h *TokenHandle) Revoke(c *gin.Context) {
	if h.svc == nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, aienums.ErrInternal)
		return
	}
	var body tokenRevokeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), body.ID); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"id": body.ID})
}
