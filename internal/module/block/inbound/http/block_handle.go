package blockhttp

// block_handle.go — 全局块 REST API 的 handler 与错误映射（JSON 模式）。
//
// 注册落点在 block_router.go；页面侧（/admin/blocks）在 block_page.go 与 block_page_router.go。

import (
	"errors"
	"net/http"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockservice "go_wp/internal/module/block/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 全局块 HTTP 处理器。
type Handle struct {
	svc blockcontract.BlockService
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
		paramBindFail(c, err)
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
		paramBindFail(c, err)
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
		paramBindFail(c, err)
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
		paramBindFail(c, err)
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
		paramBindFail(c, err)
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

// paramBindFail 请求绑定失败的统一出口（400 + 受控文案）。
//
// 不把绑定错误原文拼进响应：gin 的绑定错误会带上 Go 结构体与字段名
// （如 `json: cannot unmarshal string into Go struct field CreateReq.title of type string`），
// 那是实现细节 —— 对外只说「参数不合法」，原文进日志供排障。
//
// 原先这里是 `response.ParamError(c, err.Error())`。它一直待在门禁盲区里：形态 ① 只认
// `*.ErrorWithMessage(` 与 `c.String(`，而 `ParamError` 是同一个包里的同族出口却不在判据里。
// 2026-09 第三批把 ParamError 纳入判据后，本模块这几处立刻被扫出来 —— 判据是**形状**，
// 不是字面量；同族出口漏一个就等于那一族都没管住。
func paramBindFail(c *gin.Context, err error) {
	if err != nil {
		// 绑定失败是客户端输入问题，按 warn 记（不污染错误日志）。
		logger.Scene("block").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("block 接口请求绑定失败")
	}
	response.ParamError(c)
}
