// ai_handle.go — ai 模块的 JSON 接口（参数绑定 → service → 统一响应）。
//
// 只做绑定与响应：文案一律取 enums，业务判断在 service。列表 / 查询走 GET + Query 参数，
// 写入走 POST（路由里没有路径参数）。
package aihttp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	aicontract "go_wp/internal/module/ai/contract"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/response"
)

// Handle JSON 接口的处理器。
type Handle struct {
	svc aicontract.AIService
}

// NewHandle 构造处理器。
func NewHandle(svc aicontract.AIService) *Handle { return &Handle{svc: svc} }

// ListProviders GET /api/ai/provider/list → 供应商列表（含模型目录，不含密钥明文）。
func (h *Handle) ListProviders(c *gin.Context) {
	rows, err := h.svc.ListProviders(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "ai", err)
		return
	}
	response.Success(c, gin.H{"list": rows})
}

// GetProvider GET /api/ai/provider/get?id= → 单个供应商。
func (h *Handle) GetProvider(c *gin.Context) {
	id := parseInt64(c.Query("id"))
	if id <= 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "ai", err)
		return
	}
	response.Success(c, provider)
}

// saveProviderBody POST /api/ai/provider/save 的请求体。
type saveProviderBody struct {
	ID          int64  `json:"id"`
	ProviderKey string `json:"providerKey"`
	DisplayName string `json:"displayName"`
	BaseURL     string `json:"baseUrl"`
	Protocol    string `json:"protocol"`
	APIKey      string `json:"apiKey"`
	Status      *int   `json:"status"`
	Sort        *int   `json:"sort"`
	Version     int64  `json:"version"`
}

// SaveProvider POST /api/ai/provider/save → 新建（id 缺省 / 0）或按版本号更新。
func (h *Handle) SaveProvider(c *gin.Context) {
	var body saveProviderBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	req := &aidto.SaveProviderReq{
		ID:          body.ID,
		ProviderKey: body.ProviderKey,
		DisplayName: body.DisplayName,
		BaseURL:     body.BaseURL,
		Protocol:    body.Protocol,
		APIKey:      body.APIKey,
		Status:      body.Status,
		Version:     body.Version,
		UpdateBy:    userID(c),
	}
	if body.Sort != nil {
		req.Sort = *body.Sort
	}
	provider, err := h.svc.SaveProvider(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSaved, provider)
}

// DeleteProvider POST /api/ai/provider/delete?id= → 删除供应商（含模型目录）。
func (h *Handle) DeleteProvider(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	if err = h.svc.DeleteProvider(c.Request.Context(), id); err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgDeleted, nil)
}

// setStatusBody POST /api/ai/provider/status 的请求体。
type setStatusBody struct {
	ID      int64 `json:"id"`
	Status  int   `json:"status"`
	Version int64 `json:"version"`
}

// SetProviderStatus POST /api/ai/provider/status → 启停供应商。
func (h *Handle) SetProviderStatus(c *gin.Context) {
	var body setStatusBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.SetProviderStatus(c.Request.Context(), &aidto.SetStatusReq{
		ID:       body.ID,
		Status:   body.Status,
		Version:  body.Version,
		UpdateBy: userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgStatusChanged, provider)
}

// ListModels GET /api/ai/provider/models/list?id= → 某供应商的模型目录。
func (h *Handle) ListModels(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "ai", err)
		return
	}
	response.Success(c, gin.H{"providerId": provider.ID, "list": provider.Models})
}

// saveModelsBody POST /api/ai/provider/models/save 的请求体。
type saveModelsBody struct {
	ProviderID int64              `json:"providerId"`
	Version    int64              `json:"version"`
	Models     []aidto.ModelEntry `json:"models"`
}

// SaveModels POST /api/ai/provider/models/save → 整组保存模型目录。
func (h *Handle) SaveModels(c *gin.Context) {
	var body saveModelsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.SaveModels(c.Request.Context(), &aidto.SaveModelsReq{
		ProviderID: body.ProviderID,
		Version:    body.Version,
		Models:     body.Models,
		UpdateBy:   userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgModelsSaved, provider)
}

// providerActionBody 「恢复默认模型」「获取可用模型」的请求体。
type providerActionBody struct {
	ProviderID int64 `json:"providerId"`
	Version    int64 `json:"version"`
}

// RestoreDefaultModels POST /api/ai/provider/models/restore → 用内置清单替换模型目录。
func (h *Handle) RestoreDefaultModels(c *gin.Context) {
	var body providerActionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.RestoreDefaultModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: body.ProviderID,
		Version:    body.Version,
		UpdateBy:   userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgModelsRestored, provider)
}

// FetchAvailableModels POST /api/ai/provider/models/fetch → 调 {base_url}/models 拉候选并合并。
func (h *Handle) FetchAvailableModels(c *gin.Context) {
	var body providerActionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	result, err := h.svc.FetchAvailableModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: body.ProviderID,
		Version:    body.Version,
		UpdateBy:   userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgModelsFetched, result)
}

// bindID 绑定查询参数里的 id（Query 优先，兼容表单）。
func bindID(c *gin.Context) (int64, error) {
	if id := parseInt64(c.Query("id")); id > 0 {
		return id, nil
	}
	if id := parseInt64(c.PostForm("id")); id > 0 {
		return id, nil
	}
	return 0, errors.New(aienums.ErrInvalidParam)
}
