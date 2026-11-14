package userhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
)

// customerWriteResult 是单条客户动作的成功提示：走统一提示组件，按词条取词。
func customerWriteResult(c *gin.Context, key string) {
	pageview.SuccessMessage(c, key, customerNoticeFallback[key])
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
	form, err := binding.ParseForm[userdto.CustomerBulkForm](c)
	if err != nil {
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
	form, err := binding.ParseForm[userdto.CustomerBulkForm](c)
	if err != nil {
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
