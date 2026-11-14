package blockhttp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockservice "go_wp/internal/module/block/service"
	"go_wp/pkg/binding"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// blockWriteOK 是写操作的统一成功片段。
func blockWriteOK(c *gin.Context) {
	pageview.Success(c)
}

// parseBlockInt 把表单里的整数字串转成 int；空串或非法返回 0。
func parseBlockInt(text string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

type Handle struct {
	svc blockcontract.BlockService
}

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

func (h *Handle) Detail(c *gin.Context) {
	req, ok := binding.Query[blockdto.DetailReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// Create 接收标准表单：普通字段由页面控件提交，成功后由 Jet 返回包含下一步链接的片段。
func (h *Handle) Create(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form blockdto.CreateForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockParamRequired))
		return
	}
	// 表单字段是字符串，预设组合的取值语义在这里显式核对：
	// 有预设必须给出 ≥1 的版本，没有预设不得单独给版本（与复杂 JSON 输入同一条规则）。
	presetVersion := parseBlockInt(form.PresetVersion)
	switch {
	case form.PresetKey != "" && presetVersion < 1:
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockPresetInvalid))
		return
	case form.PresetKey == "" && presetVersion != 0:
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockPresetInvalid))
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &blockdto.CreateReq{
		ProjectID:     form.ProjectID,
		Name:          form.Name,
		Kind:          form.Kind,
		Category:      form.Category,
		ReuseMode:     form.ReuseMode,
		PresetKey:     form.PresetKey,
		PresetVersion: int(presetVersion),
	})
	if err != nil {
		pageview.FailScene(c, blockErrScene, err)
		return
	}
	c.HTML(http.StatusOK, "admin/block/block_created.html", pageview.Objects(
		pageview.Object("Name", res.Name),
		pageview.Object("ID", res.ID),
	))
}

// Update 保留复杂编辑器的明确 JSON 输入，输出仍是 Jet 片段。
func (h *Handle) Update(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseJSON[blockdto.UpdateReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockParamRequired))
		return
	}
	if _, err := h.svc.Update(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, blockErrScene, err)
		return
	}
	blockWriteOK(c)
}

func (h *Handle) Delete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form blockdto.DeleteForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockParamRequired))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &blockdto.DeleteReq{ID: form.ID}); err != nil {
		pageview.FailScene(c, blockErrScene, err)
		return
	}
	blockWriteOK(c)
}

func (h *Handle) BulkDelete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form blockdto.BulkDeleteForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockParamRequired))
		return
	}
	// 表单编码下「完全没有 ids」与「ids 全为空」不相等：前者是缺形状，后者是空勾选。
	if _, present := c.Request.PostForm["ids"]; !present {
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockParamRequired))
		return
	}
	ids, nerr := bulkids.Normalize(form.IDs)
	if nerr != nil {
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, nerr)
		return
	}
	if err := h.svc.BulkDelete(c.Request.Context(), &blockdto.BulkDeleteReq{IDs: ids}); err != nil {
		pageview.FailScene(c, blockErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *Handle) CloneAST(c *gin.Context) {
	var req blockdto.CloneReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.CloneAST(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

func blockErrorStatus(err error) int {

	if status, ok := facing.StatusOf(err); ok {
		return status
	}
	switch {
	case errors.Is(err, blockservice.ErrNotFound), errors.Is(err, blockservice.ErrProjectNotFound):
		return http.StatusNotFound
	case errors.Is(err, blockservice.ErrDuplicate):
		return http.StatusConflict
	case errors.Is(err, blockservice.ErrBlockInUse):

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

func blockErrorMessage(err error) string {
	if blockErrorStatus(err) == http.StatusInternalServerError {
		logger.Scene("block").Error(err, "block 接口内部错误")
		return blockenums.MsgInternalError
	}
	return err.Error()
}

func paramBindFail(c *gin.Context, err error) {
	if err != nil {

		logger.Scene("block").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("block 接口请求绑定失败")
	}
	response.ParamError(c)
}
