package mediahttp

import (
	"net/http"
	"strconv"

	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 媒体模块 HTTP 处理器。
type Handle struct {
	svc mediacontract.MediaService
}

// NewHandle 创建 HTTP 处理器。
func NewHandle(svc mediacontract.MediaService) *Handle {
	return &Handle{svc: svc}
}

// Upload 上传文件。
func (h *Handle) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.ParamError(c, mediaenums.ErrUploadEmpty)
		return
	}

	var categoryID *uint64
	if raw := c.PostForm("category_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err == nil {
			categoryID = &id
		}
	}

	att, err := h.svc.Upload(c.Request.Context(), file, categoryID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, att)
}

// List 分页查询附件列表。
func (h *Handle) List(c *gin.Context) {
	var req mediadto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return
	}

	resp, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, resp)
}

// Detail 查询附件详情。
func (h *Handle) Detail(c *gin.Context) {
	var req mediadto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return
	}

	att, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.Success(c, att)
}

// Delete 删除附件。
func (h *Handle) Delete(c *gin.Context) {
	var req mediadto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}

	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, nil)
}

// CategoryTree 获取文件分类树。
func (h *Handle) CategoryTree(c *gin.Context) {
	tree, err := h.svc.CategoryTree(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, tree)
}

// CategoryCreate 新建分类（POST /api/media/category/create）。
func (h *Handle) CategoryCreate(c *gin.Context) {
	var req mediadto.CategoryCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	res, err := h.svc.CreateCategory(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// CategoryUpdate 更新分类（POST /api/media/category/update）。
func (h *Handle) CategoryUpdate(c *gin.Context) {
	var req mediadto.CategoryUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if err := h.svc.UpdateCategory(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil)
}

// CategoryDelete 删除分类（POST /api/media/category/delete）。
func (h *Handle) CategoryDelete(c *gin.Context) {
	var req mediadto.CategoryDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil)
}

// UpdateAttachment 更新附件元数据（POST /api/media/update）。
func (h *Handle) UpdateAttachment(c *gin.Context) {
	var req mediadto.AttachmentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if err := h.svc.UpdateAttachment(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, nil)
}
