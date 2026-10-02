package pagehttp

import (
	"errors"
	"net/http"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle page 模块 HTTP 处理器。
type Handle struct {
	svc pagecontract.PageService
}

// NewHandle 创建 page HTTP 处理器。
func NewHandle(svc pagecontract.PageService) *Handle {
	return &Handle{svc: svc}
}

// Create 创建 Page、初始 Draft 和初始 Revision。
func (h *Handle) Create(c *gin.Context) {
	var req pagedto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageCreated, res)
}

// Detail 查询 Page 当前草稿。
func (h *Handle) Detail(c *gin.Context) {
	var req pagedto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDetail, res)
}

// List 列出全部页面摘要。
func (h *Handle) List(c *gin.Context) {
	var req pagedto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// SaveDraft 保存完整 Page AST 并追加不可变 Revision。
func (h *Handle) SaveDraft(c *gin.Context) {
	var req pagedto.SaveDraftReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SaveDraft(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgDraftSaved, res)
}

// Build 基于当前草稿构建并暂存产物。
func (h *Handle) Build(c *gin.Context) {
	var req pagedto.BuildReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Build(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgBuildReady, res)
}

// Publish 激活暂存产物；allLangs=true 时一键发布全部启用语言（按语言逐条聚合结果）。
func (h *Handle) Publish(c *gin.Context) {
	var req pagedto.PublishReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	if req.AllLangs {
		// 一键发布全部启用语言：进了编排就返回 200，失败明细按语言逐条在
		// results 里（单语言失败不阻断其余语言，与 rebuildPage 同一口径）。
		res, err := h.svc.PublishAllLanguages(c.Request.Context(), &req)
		if err != nil {
			response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
			return
		}
		response.SuccessWithMessage(c, pageenums.MsgPublished, res)
		return
	}
	res, err := h.svc.Publish(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPublished, res)
}

// RebuildArtifact 按产物元数据重建丢失的产物文件（灾难恢复）。
func (h *Handle) RebuildArtifact(c *gin.Context) {
	var req pagedto.RebuildArtifactReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.RebuildArtifact(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// GarbageCollectArtifacts 回收超期且无引用的产物文件（默认 dryRun，先看再删）。
func (h *Handle) GarbageCollectArtifacts(c *gin.Context) {
	var req pagedto.GCArtifactsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		// 允许空 body：全部走默认值（30 天保留窗口 + dryRun）
		req = pagedto.GCArtifactsReq{}
	}
	res, err := h.svc.GarbageCollectArtifacts(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// SEOPatrol 站点 SEO 合规巡检（审计 SEO-01）：只读，按激活清单逐份校验产物字节，
// 返回 URL / 规则 / 证据 / ArtifactHash 齐备的报告。
func (h *Handle) SEOPatrol(c *gin.Context) {
	var req pagedto.SEOPatrolReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SEOPatrol(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// AuditPublication 巡检激活面：返回所有悬空/异常的激活链接。
func (h *Handle) AuditPublication(c *gin.Context) {
	res, err := h.svc.AuditPublication(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// Rollback 回滚到历史产物。
func (h *Handle) Rollback(c *gin.Context) {
	var req pagedto.RollbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Rollback(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRollbackDone, res)
}

// UpdateURL 修改访问路径，旧路径 301 或取消激活。
func (h *Handle) UpdateURL(c *gin.Context) {
	var req pagedto.UpdateURLReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateURL(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgURLUpdated, res)
}

// Delete 软删页面并释放其全部路径占用。
func (h *Handle) Delete(c *gin.Context) {
	var req pagedto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDeleted, nil)
}

// ListRevisions 查询 Page 草稿修订历史。
func (h *Handle) ListRevisions(c *gin.Context) {
	var req pagedto.RevisionReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListRevisions(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRevisionsListed, res)
}

// pageErrorStatus 把 page service 返回的错误分类映射为 HTTP 状态码。
// 修复前按 err.Error() 中文文案 strings.Contains 匹配（文案改动即失效）；
// 修复后基于本包 sentinel error 精确 errors.Is 判定（文案与 pageenums 一致）。
// ErrInvalidParam（nil/空 ID 等参数错误）映射 400，与资源不存在（404）区分。
func pageErrorStatus(err error) int {
	switch {
	case errors.Is(err, pageservice.ErrNoStagedArtifact):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrPageNotFound),
		errors.Is(err, pageservice.ErrProjectNotFound),
		errors.Is(err, pageservice.ErrRollbackTargetMiss):
		return http.StatusNotFound
	case errors.Is(err, pageservice.ErrDraftVersionConflict),
		errors.Is(err, pageservice.ErrPathOccupied),
		errors.Is(err, pageservice.ErrRebuildRequired),
		// 定时上下线（PIPE-7）：正在执行的排定不能被取消 —— 与「草稿已变更」同属
		// 「当前状态与请求冲突，刷新后重试」，而不是调用方写错了参数。
		errors.Is(err, pageservice.ErrScheduleOccupied),
		errors.Is(err, pageservice.ErrScheduleRunning):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrScheduleNotFound):
		return http.StatusNotFound
	// 页面级语言排除（迁移 491）：「这一页不产出该语言」/「默认语言不能排除」都是
	// **当前状态与请求冲突**（刷新后能看到真实状态），不是调用方写错参数，
	// 也不是内部故障 —— 归到 409 与排定占用同一档，前端据此给"可展示的业务文案"。
	case errors.Is(err, pageservice.ErrPageLangExcluded),
		errors.Is(err, pageservice.ErrCannotExcludeDefaultLang),
		errors.Is(err, pageservice.ErrLangAlreadyExcluded),
		errors.Is(err, pageservice.ErrLangNotExcluded):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrInvalidParam),
		errors.Is(err, pageservice.ErrInvalidKind),
		errors.Is(err, pageservice.ErrInvalidDocument),
		errors.Is(err, pageservice.ErrInvalidPath),
		// DB-009 第三批：跨工程扇出入口在「工程表读不到 / 一个工程都没有」时显式失败，
		// 这是调用环境的问题（400）而不是内部故障（500）—— 它不该被兜底文案吞掉。
		errors.Is(err, pageservice.ErrProjectRequired),
		// 定时上下线（PIPE-7）：排定时间在过去 / 动作不在白名单内都属于请求本身不合法。
		errors.Is(err, pageservice.ErrScheduleInPast),
		errors.Is(err, pageservice.ErrScheduleActionInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// pageErrorMessage 把 service 错误映射为响应消息：
// 已知业务错误（sentinel）按当前语言给出文案 —— **与页面出口（pageFacingText）同一判据、
// 同一明细协议**，两个出口对同一个错误给出同一个结论；未知系统错误（pageErrorStatus 归为 500）
// 改用兜底文案下发，原文只进日志，避免 err.Error() 把内部细节（SQL 错误、连接信息）泄露给客户端。
//
// 改造前这里是非 500 直接 `err.Error()`：业务 key 是裸 key（英文界面显示 ErrInvalidDocument）、
// 明细是 builder 的中文原文（`ErrInvalidDocument: 顶级节点 0: 组件树深度 11 超过上限 10…`）。
// 现在借用 pageFacingText 把两截都按当前语言渲染，明细不是 ErrorDetail 词条时丢弃并落日志。
func pageErrorMessage(c *gin.Context, err error) string {
	if text, ok := pageFacingText(c, err); ok {
		return text
	}
	if err != nil {
		// 结构化日志带 user_id：事后才能回答「谁点了哪个按钮」。
		logger.Scene(pageErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "page 接口内部错误")
	}
	return pageenums.MsgInternalError
}

// ListSiteSlots 列出系统页面槽位及其绑定状态（含未绑定的槽位）。
func (h *Handle) ListSiteSlots(c *gin.Context) {
	var req pagedto.SiteSlotListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListSiteSlots(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDetail, res)
}

// BindSiteSlot 把系统页面槽位绑到某个页面。
//
// 用 ShouldBind 而不是 ShouldBindJSON：后台页是原生表单 POST（与其它后台页一致），
// 同时 API 调用方发 JSON 也走同一入口 —— DTO 的 json / form tag 同名同义。
func (h *Handle) BindSiteSlot(c *gin.Context) {
	var req pagedto.SiteSlotBindReq
	if err := c.ShouldBind(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	if err := h.svc.BindSiteSlot(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgSiteSlotBound, nil)
}

// UnbindSiteSlot 解绑系统页面槽位（幂等：本来没绑也返回成功）。
func (h *Handle) UnbindSiteSlot(c *gin.Context) {
	var req pagedto.SiteSlotUnbindReq
	if err := c.ShouldBind(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	if err := h.svc.UnbindSiteSlot(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgSiteSlotUnbound, nil)
}
