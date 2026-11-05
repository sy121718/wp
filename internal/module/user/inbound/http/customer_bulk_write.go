package userhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/adminui"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

type customerBulkItem struct {
	ID       uint64 `json:"id"`
	Updated  bool   `json:"updated"`
	Unlocked bool   `json:"unlocked"`
	Cleared  bool   `json:"cleared"`
	Noop     bool   `json:"noop"`
	Message  string `json:"message"`
}

type customerBulkResult struct {
	Updated  int                `json:"updated"`
	Skipped  int                `json:"skipped"`
	Unlocked int                `json:"unlocked"`
	Cleared  int                `json:"cleared"`
	Noop     int                `json:"noop"`
	Items    []customerBulkItem `json:"items"`
}

// customerWriteResult 是单条客户动作的成功提示：走统一提示组件，按词条取词。
func customerWriteResult(c *gin.Context, key string) {
	pageview.SuccessMessage(c, key, customerNoticeFallback[key])
}

func customerAPIErrorText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	return adminui.PageErrorText(c, "user_customer_write", err)
}

func customerAPIWriteError(c *gin.Context, err error) {
	code := http.StatusBadRequest
	if !response.IsBusinessError(err) {
		code = http.StatusInternalServerError
	}
	response.ErrorWithMessage(c, code, customerAPIErrorText(c, err))
}

func customerUnlockMessage(res *userdto.CustomerUnlockResp) string {
	if res.Unlocked {
		return userenums.MsgCustomerUnlocked
	}
	if res.Cleared {
		return userenums.MsgCustomerFailuresCleared
	}
	return userenums.MsgCustomerNotLocked
}

func (h *CustomerHandle) BulkCustomerStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form userdto.CustomerBulkForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		customerBulkFail(c, userenums.Err(userenums.ErrInvalidParam))
		return
	}
	ids, err := form.CustomerBulkIDs()
	if err != nil {
		customerBulkFail(c, err)
		return
	}
	status, err := form.CustomerBulkStatusValue()
	if err != nil {
		customerBulkFail(c, err)
		return
	}
	if err := h.svc.BulkSetCustomerStatus(c.Request.Context(), &userdto.CustomerBulkStatusReq{
		IDs: ids, Status: &status,
	}); err != nil {
		pageview.FailScene(c, userCustomerErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *CustomerHandle) BulkCustomerUnlock(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form userdto.CustomerBulkForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		customerBulkFail(c, userenums.Err(userenums.ErrInvalidParam))
		return
	}
	ids, err := form.CustomerBulkIDs()
	if err != nil {
		customerBulkFail(c, err)
		return
	}
	if err := h.svc.BulkUnlockCustomers(c.Request.Context(), &userdto.CustomerBulkUnlockReq{IDs: ids}); err != nil {
		pageview.FailScene(c, userCustomerErrScene, err)
		return
	}
	pageview.Success(c)
}

// customerBulkFail 把批量请求的失败事实交给共享 HTML 出口。
func customerBulkFail(c *gin.Context, err error) {
	pageview.FailStatus(c, http.StatusBadRequest, userCustomerErrScene, err)
}

func customerBulkHTML(c *gin.Context, template string, done, skipped int, rows []map[string]any) {
	c.HTML(http.StatusOK, template, pageview.Objects(
		pageview.Object("Done", done),
		pageview.Object("Skipped", skipped),
		pageview.Object("Rows", rows),
	))
}
