// Package contenttemplatehttp contenttemplate 模块 HTTP 入口（0-A2）。
package contenttemplatehttp

import (
	"errors"
	"fmt"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"

	"go_wp/pkg/bulkids"

	"github.com/gin-gonic/gin"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	"go_wp/pkg/response"
)

// Handle contenttemplate 接口处理器。
type Handle struct {
	svc contenttemplatecontract.ContentTemplateService
}

// NewHandle 构造。
func NewHandle(svc contenttemplatecontract.ContentTemplateService) *Handle {
	return &Handle{svc: svc}
}

// Create 新建模板。
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

// Update 更新模板。
// Activate 切换生效模板（POST /api/contenttemplate/activate）。
func (h *Handle) Activate(c *gin.Context) {
	req, ok := binding.JSON[contenttemplatedto.ActivateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Activate(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.Success(c, res)
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

// Delete accepts one identifier or a normalized batch and reports each result.
func (h *Handle) Delete(c *gin.Context) {
	raw, ok := binding.JSON[map[string]json.RawMessage](c)
	if !ok {
		return
	}
	if *raw == nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	fields := *raw
	var ids []string
	idValue, single := fields["id"]
	idsValue, batch := fields["ids"]
	if single == batch || len(fields) != 1 {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	if single {
		var id string
		if err := json.Unmarshal(idValue, &id); err != nil || strings.TrimSpace(id) == "" {
			response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
			return
		}
		ids = []string{id}
	} else if err := json.Unmarshal(idsValue, &ids); err != nil || string(idsValue) == "null" {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	normalized, err := bulkids.Normalize(ids)
	if err != nil {
		var limit *bulkids.IDsError
		if errors.As(err, &limit) {
			message := fmt.Sprintf(i18n.Translate(bulkids.MsgTooMany, bulkids.TooManyTemplate, response.RequestLanguage(c)), strconv.Itoa(limit.Max), strconv.Itoa(limit.Count))
			response.ErrorWithMessage(c, http.StatusBadRequest, message)
			return
		}
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	result := contenttemplatedto.BulkDeleteResp{Items: make([]contenttemplatedto.BulkDeleteItemResult, 0, len(normalized))}
	for _, id := range normalized {
		item := contenttemplatedto.BulkDeleteItemResult{ID: id}
		if err := h.svc.Delete(c.Request.Context(), &contenttemplatedto.DeleteReq{ID: id}); err != nil {
			item.Message = contentTemplateErrText(c, err)
			result.Skipped++
		} else {
			item.Deleted = true
			result.Deleted++
		}
		result.Items = append(result.Items, item)
	}
	response.Success(c, result)
}

// Get 模板详情。
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

// List 模板列表。
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
