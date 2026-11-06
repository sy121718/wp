package orderhttp

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	pageview "go_wp/pkg/view"
)

// couponTimeInputValue 把浏览器 datetime-local 的值规整成数据库接受的时刻串。
func couponTimeInputValue(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return s
}

// couponDeleteBulk 是勾选多张优惠码的批量删除：表单原样交给 service 一次调用，
// 整批同一事务；失败只回一句公共文案，逐条原因走 BlockedItems 的数据出口。
func (h *Handle) couponDeleteBulk(c *gin.Context) {
	var form orderdto.CouponBulkDeleteReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if _, err := h.svc.DeleteCoupons(c.Request.Context(), &form); err != nil {
		orderLogWriteError(c, err, "批量删除优惠码失败")
		orderWriteFail(c, err)
		return
	}
	// 普通写成功只回公共一句：批量要么全成、要么全败，不展示逐条回执。
	pageview.Success(c)
}

// couponToggleBulk 是勾选多张优惠码的批量启停：一次调用只改 status，
// 不整行覆盖名称、门槛、时间窗等其它字段。
func (h *Handle) couponToggleBulk(c *gin.Context) {
	var form orderdto.CouponBulkStatusForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	target, ok := couponToggleTarget(form.Status)
	if !ok {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	operatorID, _ := operatorFromContext(c)
	req := &orderdto.CouponBulkStatusReq{
		IDs:        form.IDs,
		ProjectID:  form.ProjectID,
		Status:     target,
		OperatorID: operatorID,
	}
	if _, err := h.svc.SetCouponsStatus(c.Request.Context(), req); err != nil {
		orderLogWriteError(c, err, "批量启停优惠码失败")
		orderWriteFail(c, err)
		return
	}
	pageview.Success(c)
}

// couponToggleTarget 只接受 "0" / "1"：表单取值是字符串，语义是启停。
func couponToggleTarget(raw string) (int, bool) {
	switch strings.TrimSpace(raw) {
	case "0":
		return 0, true
	case "1":
		return 1, true
	default:
		return 0, false
	}
}
