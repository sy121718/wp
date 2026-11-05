package contenttemplatehttp

import (
	"errors"
	"fmt"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"net/http"
	"strconv"
	"strings"

	"go_wp/pkg/bulkids"
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
	req, ok := binding.JSON[contenttemplatedto.CreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
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
	req, ok := binding.JSON[contenttemplatedto.UpdateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgUpdateSuccess, res)
}

// Delete 接收标准表单：id 表示单条，ids 表示批量；两种形态互斥。
// 逐条结果作为业务事实交 Jet 片段，失败不再落到 JSON 出口。
func (h *Handle) Delete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ids, ok := contentTemplateDeleteIDs(c)
	if !ok {
		return
	}
	normalized, err := bulkids.Normalize(ids)
	if err != nil {
		var limit *bulkids.IDsError
		if errors.As(err, &limit) {
			message := fmt.Sprintf(i18n.Translate(bulkids.MsgTooMany, bulkids.TooManyTemplate, response.RequestLanguage(c)), strconv.Itoa(limit.Max), strconv.Itoa(limit.Count))
			pageview.FailStatus(c, http.StatusBadRequest, "contenttemplate", errors.New(message))
			return
		}
		pageview.FailStatus(c, http.StatusBadRequest, "contenttemplate", contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam))
		return
	}
	result := contenttemplatedto.BulkDeleteResp{Items: make([]contenttemplatedto.BulkDeleteItemResult, 0, len(normalized))}
	errs := make([]error, 0, len(normalized))
	for _, id := range normalized {
		item := contenttemplatedto.BulkDeleteItemResult{ID: id}
		delErr := h.svc.Delete(c.Request.Context(), &contenttemplatedto.DeleteReq{ID: id})
		if delErr != nil {
			item.Message = delErr.Error()
			result.Skipped++
		} else {
			item.Deleted = true
			result.Deleted++
		}
		result.Items = append(result.Items, item)
		errs = append(errs, delErr)
	}
	c.HTML(http.StatusOK, "admin/contenttemplate/content_template_delete_result.html", pageview.Objects(
		pageview.Object("Result", result),
		pageview.Object("Rows", contentTemplateDeleteRows(c, result, errs)),
	))
}

// contentTemplateDeleteIDs 只读标准表单编码，并拒绝同时提交 id 与 ids。
func contentTemplateDeleteIDs(c *gin.Context) ([]string, bool) {
	if err := c.Request.ParseForm(); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "contenttemplate", err)
		return nil, false
	}
	form := c.Request.PostForm
	id := strings.TrimSpace(form.Get("id"))
	batch := form["ids"]
	if (id == "") == (len(batch) == 0) {
		pageview.FailStatus(c, http.StatusBadRequest, "contenttemplate", contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam))
		return nil, false
	}
	if id != "" {
		return []string{id}, true
	}
	return batch, true
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
