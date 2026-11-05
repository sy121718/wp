package userhttp

import (
	"net/http"
	"strconv"

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

// customerWriteResult 是单条客户动作的成功片段：句子只在 Jet 里，按词条取词；
// Fallback 只在缺词时兜底，绝不让裸 key 上屏。
func customerWriteResult(c *gin.Context, key string) {
	c.HTML(http.StatusOK, "admin/user/customer_write_result.html", pageview.Objects(
		pageview.Object("Key", key),
		pageview.Object("Fallback", customerNoticeFallback[key]),
	))
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
	updated, skipped := 0, 0
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		row := map[string]any{"ID": strconv.FormatUint(id, 10), "OK": false, "Message": ""}
		res, serr := h.svc.SetCustomerStatus(c.Request.Context(), &userdto.CustomerStatusReq{CustomerID: id, Status: status})
		switch {
		case serr != nil:
			row["Message"] = customerAPIErrorText(c, serr)
			skipped++
		case res == nil:
			row["Message"] = response.TranslateMessage(c, userenums.ErrInternal)
			skipped++
		default:
			row["OK"] = true
			updated++
		}
		rows = append(rows, row)
	}
	customerBulkHTML(c, "admin/user/customer_bulk_status_result.html", updated, skipped, rows)
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
	unlocked, cleared, noop, skipped := 0, 0, 0, 0
	for _, id := range ids {
		res, serr := h.svc.UnlockCustomer(c.Request.Context(), &userdto.CustomerUnlockReq{CustomerID: id})
		switch {
		case serr != nil:
			skipped++
		case res == nil:
			skipped++
		case res.Unlocked:
			unlocked++
		case res.Cleared:
			cleared++
		default:
			noop++
		}
	}
	c.HTML(http.StatusOK, "admin/user/customer_bulk_unlock_result.html", pageview.Objects(
		pageview.Object("Unlocked", unlocked),
		pageview.Object("Cleared", cleared),
		pageview.Object("Noop", noop),
		pageview.Object("Skipped", skipped),
	))
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
