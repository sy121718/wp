// product_tag_handle.go — 商品标签 HTTP 入口（issue #11）。
//
// 与分类 / 品牌接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：规则校验、重算时机、引用校验一律在 service。
package producthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/response"
)

// CreateTag 新建标签（kind=manual 手工 / kind=rule 自动并带内置规则与参数）。
func (h *Handle) CreateTag(c *gin.Context) {
	req := &productdto.CreateTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateTag 修改标签（改名 / 换 slug / 换类型 / 改规则参数 / 排序）。
func (h *Handle) UpdateTag(c *gin.Context) {
	req := &productdto.UpdateTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetTag 标签详情（含命中商品列表）。
func (h *Handle) GetTag(c *gin.Context) {
	req := &productdto.GetTagReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListTags 标签列表（工程内；每个标签带当前归属数量）。
func (h *Handle) ListTags(c *gin.Context) {
	req := &productdto.ListTagReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagProducts 某标签命中的商品（验收 4）。
func (h *Handle) ListTagProducts(c *gin.Context) {
	req := &productdto.ListTagProductsReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListTagProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagRuleTypes 内置规则类型清单（后台规则下拉的唯一来源）。
func (h *Handle) ListTagRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListTagRuleTypes(c.Request.Context()))
}

// DeleteTag 删除标签（连同它在商品上的引用一起解绑）。
func (h *Handle) DeleteTag(c *gin.Context) {
	req := &productdto.DeleteTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteTag(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// RecalcTags 手动触发重算（重算时机之一：可按标签或按工程整体重算）。
func (h *Handle) RecalcTags(c *gin.Context) {
	req := &productdto.RecalcTagsReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.RecalcTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}
