// masterdata_handle.go — 主数据变更记录 HTTP 入口（issue #19）。
//
// 与其它模块同形：GET + Query 参数（无 RESTful 路径参数）、pkg/response 出参、
// 消息取模块 enums。本层只做绑定与转发，校验与查询在 service。
//
// 只读模块：没有任何写接口 —— 变更记录由业务模块在写操作里经契约追加，
// 对外不提供「手工补一条记录」的口子（审计的入口越少越可信）。
package masterdatahttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	"go_wp/pkg/response"
)

// Handle 变更记录 HTTP 处理器。
type Handle struct {
	svc masterdatacontract.MasterDataService
}

// NewHandle 构造。
func NewHandle(svc masterdatacontract.MasterDataService) *Handle { return &Handle{svc: svc} }

// ListChanges 按条件查字段级变更（实体类型 / 实体 id / 字段 / 动作 / 操作人 / 时间窗）。
func (h *Handle) ListChanges(c *gin.Context) {
	req := &masterdatadto.ListChangeReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, masterdataenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListChanges(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, list)
}

// CountChanges 同条件的总条数（分页用）。
func (h *Handle) CountChanges(c *gin.Context) {
	req := &masterdatadto.ListChangeReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, masterdataenums.ErrInvalidParam)
		return
	}
	total, err := h.svc.CountChanges(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, gin.H{"total": total})
}

// ListEntities 按实体聚合的变更历史清单（验收 4 的入口）。
func (h *Handle) ListEntities(c *gin.Context) {
	req := &masterdatadto.ListEntityReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, masterdataenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListEntities(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, list)
}

// EntityTimeline 单个实体的完整变更历史（实体类型 + 实体 id）。
func (h *Handle) EntityTimeline(c *gin.Context) {
	req := &masterdatadto.EntityTimelineReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, masterdataenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.EntityTimeline(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, masterdataenums.MsgListSuccess, res)
}
