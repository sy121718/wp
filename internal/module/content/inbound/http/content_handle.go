// Package contenthttp content 模块 HTTP 入口（0-A2）。
package contenthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	"go_wp/pkg/response"
)

// Handle content 接口处理器。
type Handle struct {
	svc contentcontract.ContentService
	// collections 集合源元数据聚合端口（装配期注入的集合源注册表，可空）。
	//
	// 集合源不再只属于内容模块（商品也是集合源，issue #9）：本接口是工作台与
	// 内置组件拿集合源元数据的唯一入口，必须返回全量集合源而不是本模块那几条。
	collections core.CollectionSchemaProvider
}

// NewHandle 构造。
func NewHandle(svc contentcontract.ContentService) *Handle { return &Handle{svc: svc} }

// SetCollectionSchemas 注入集合源元数据聚合端口（装配期调用）。
// 传入 nil 时退回「只报告本模块的集合源」（单模块场景）。
func (h *Handle) SetCollectionSchemas(p core.CollectionSchemaProvider) { h.collections = p }

// Create 新建内容。
func (h *Handle) Create(c *gin.Context) {
	req := &contentdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgCreateSuccess, res)
}

// Update 更新内容。
func (h *Handle) Update(c *gin.Context) {
	req := &contentdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgUpdateSuccess, res)
}

// Get 内容详情。
func (h *Handle) Get(c *gin.Context) {
	req := &contentdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDetailSuccess, res)
}

// List 内容列表。
func (h *Handle) List(c *gin.Context) {
	req := &contentdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgListSuccess, list)
}

// Collections 集合源元数据（字段白名单 + 过滤维度 + 排序键）：
// 内置组件构建期校验与工作台字段下拉的唯一来源。
//
// 优先走装配期注入的集合源注册表（全量集合源，含商品等其它领域模块）；
// 未注入时退回本模块服务（只有内容集合源）。
func (h *Handle) Collections(c *gin.Context) {
	if h.collections != nil {
		items, err := h.collections.CollectionSchemas(c.Request.Context())
		if err != nil {
			response.ErrorAuto(c, http.StatusBadRequest, "content", err)
			return
		}
		response.SuccessWithMessage(c, contentenums.MsgCollectionsSuccess, gin.H{"collections": items})
		return
	}
	provider, ok := h.svc.(core.CollectionSchemaProvider)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrCollectionUnsupported)
		return
	}
	items, err := provider.CollectionSchemas(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgCollectionsSuccess, gin.H{"collections": items})
}

// Delete 删除内容。
func (h *Handle) Delete(c *gin.Context) {
	req := &contentdto.DeleteReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "content", err)
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDeleteSuccess, nil)
}
