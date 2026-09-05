package blockhttp

// 全局块 REST API（JSON 模式）：列表/详情/新建/更新/删除。

import (
	"net/http"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/response"

	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handle 全局块 HTTP 处理器。
type Handle struct {
	svc blockcontract.BlockService
}

// SetupBlockRoutes 注册全局块路由，返回块契约（供 page 构建装配与 dashboard 使用）。
func SetupBlockRoutes(rg *gin.RouterGroup, db *gorm.DB, projects projectcontract.ProjectService) blockcontract.BlockService {
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	h := &Handle{svc: svc}

	g := rg.Group("/block", builtin.SessionAuthMiddleware())
	g.GET("/list", h.List)
	g.GET("/detail", h.Detail)
	g.POST("/create", h.Create)
	g.POST("/update", h.Update)
	g.POST("/delete", h.Delete)
	return svc
}

// List 列出工程全局块（?projectId=&kind=）。
func (h *Handle) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context(), &blockdto.ListReq{
		ProjectID: c.Query("projectId"), Kind: c.Query("kind"),
	})
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), err.Error())
		return
	}
	response.Success(c, res)
}

// Detail 块详情。
func (h *Handle) Detail(c *gin.Context) {
	res, err := h.svc.Detail(c.Request.Context(), &blockdto.DetailReq{ID: c.Query("id")})
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), err.Error())
		return
	}
	response.Success(c, res)
}

// Create 新建块。
func (h *Handle) Create(c *gin.Context) {
	var req blockdto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), err.Error())
		return
	}
	response.Success(c, res)
}

// Update 更新块。
func (h *Handle) Update(c *gin.Context) {
	var req blockdto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	res, err := h.svc.Update(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), err.Error())
		return
	}
	response.Success(c, res)
}

// Delete 删除块。
func (h *Handle) Delete(c *gin.Context) {
	var req blockdto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), err.Error())
		return
	}
	response.Success(c, nil)
}

// blockErrorStatus 按错误消息映射 HTTP 状态码（沿用 page 模块 pageErrorStatus 风格）：
// 参数/校验错误 → 400，资源不存在（块/工程）→ 404，同名冲突 → 409，其余未知错误 → 500。
func blockErrorStatus(err error) int {
	message := err.Error()
	switch {
	case strings.Contains(message, blockenums.ErrBlockNotFound), strings.Contains(message, blockenums.ErrProjectNotFound):
		return http.StatusNotFound
	case strings.Contains(message, blockenums.ErrBlockDuplicate):
		return http.StatusConflict
	case strings.Contains(message, blockenums.ErrBlockParamRequired),
		strings.Contains(message, blockenums.ErrBlockNameRequired),
		strings.Contains(message, blockenums.ErrBlockInvalidDoc),
		strings.Contains(message, blockenums.ErrBlockInvalidKind):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
