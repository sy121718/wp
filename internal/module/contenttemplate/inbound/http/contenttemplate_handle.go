package contenttemplatehttp

import (
	"go_wp/pkg/binding"
	"net/http"
	"strings"

	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	"go_wp/pkg/response"
)

type Handle struct {
	svc contenttemplatecontract.ContentTemplateService
}

func NewHandle(svc contenttemplatecontract.ContentTemplateService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) Create(c *gin.Context) {
	var req contenttemplatedto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgCreateSuccess, res)
}

// Activate 接收标准表单，成功后由 Jet 返回统一成功片段。
func (h *Handle) Activate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req contenttemplatedto.ActivateReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	if _, err := h.svc.Activate(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, "contenttemplate", err)
		return
	}
	pageview.Success(c)
}

func (h *Handle) Update(c *gin.Context) {
	var req contenttemplatedto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgUpdateSuccess, res)
}

// Delete 接收标准表单：id 表示单条，ids 表示批量，两种形态互斥；
// 同一次 BulkDelete 事务完成整批删除，成功走统一成功出口，失败走共享失败出口。
func (h *Handle) Delete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form contenttemplatedto.DeleteForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, contentTemplateErrScene, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam))
		return
	}
	id := strings.TrimSpace(form.ID)
	if (id == "") == (len(form.IDs) == 0) {
		pageview.FailStatus(c, http.StatusBadRequest, contentTemplateErrScene, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam))
		return
	}
	req := &contenttemplatedto.BulkDeleteReq{IDs: form.IDs}
	if id != "" {
		req.IDs = []string{id}
	}
	if err := h.svc.BulkDelete(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, contentTemplateErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *Handle) Get(c *gin.Context) {
	req, ok := binding.Form[contenttemplatedto.GetReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgDetailSuccess, res)
}

func (h *Handle) List(c *gin.Context) {
	req, ok := binding.Form[contenttemplatedto.ListReq](c)
	if !ok {
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgListSuccess, list)
}
