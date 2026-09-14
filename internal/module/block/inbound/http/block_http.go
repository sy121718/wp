package blockhttp

// 全局块 REST API（JSON 模式）：列表/详情/新建/更新/删除。

import (
	"errors"
	"net/http"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
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
	g.POST("/clone", h.CloneAST)
	return svc
}

// List 列出工程全局块（?projectId=&kind=&category=&reuseMode=）。
func (h *Handle) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context(), &blockdto.ListReq{
		ProjectID: c.Query("projectId"), Kind: c.Query("kind"), Category: c.Query("category"),
		ReuseMode: c.Query("reuseMode"),
	})
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// Detail 块详情。
func (h *Handle) Detail(c *gin.Context) {
	var req blockdto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
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
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
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
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// Delete 删除块（global 块被引用默认拒绝，force=true 强制删除）。
func (h *Handle) Delete(c *gin.Context) {
	var req blockdto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, nil)
}

// CloneAST 复制块 AST（编辑器「插入-复制」动作）：返回与源块脱钩的独立文档。
func (h *Handle) CloneAST(c *gin.Context) {
	var req blockdto.CloneReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	res, err := h.svc.CloneAST(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// blockErrorStatus 按 sentinel error 映射 HTTP 状态码（与 page 模块 pageErrorStatus 同构）：
// 参数/校验错误 → 400，资源不存在（块/工程）→ 404，同名冲突 → 409，其余未知错误 → 500。
// 修复前按 err.Error() 中文文案 strings.Contains 匹配（文案改动即失效）；
// 修复后基于 blockservice 包 sentinel error 精确 errors.Is 判定。
func blockErrorStatus(err error) int {
	switch {
	case errors.Is(err, blockservice.ErrNotFound), errors.Is(err, blockservice.ErrProjectNotFound):
		return http.StatusNotFound
	case errors.Is(err, blockservice.ErrDuplicate):
		return http.StatusConflict
	case errors.Is(err, blockservice.ErrBlockInUse):
		// 409：资源仍被引用，属状态冲突而非参数错误。
		return http.StatusConflict
	case errors.Is(err, blockservice.ErrParamRequired),
		errors.Is(err, blockservice.ErrNameRequired),
		errors.Is(err, blockservice.ErrInvalidDoc),
		errors.Is(err, blockservice.ErrInvalidKind),
		errors.Is(err, blockservice.ErrInvalidCategory),
		errors.Is(err, blockservice.ErrInvalidReuseMode):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// blockErrorMessage 把 service 错误映射为响应消息：
// 已知业务错误（sentinel）其 Error() 即 blockenums 文案，直接下发；
// 未知系统错误（blockErrorStatus 归为 500）改用兜底文案下发，原文只进日志，
// 避免 err.Error() 把内部细节（SQL 错误、连接信息）泄露给客户端。
func blockErrorMessage(err error) string {
	if blockErrorStatus(err) == http.StatusInternalServerError {
		logger.Scene("block").Error(err, "block 接口内部错误")
		return blockenums.MsgInternalError
	}
	return err.Error()
}
