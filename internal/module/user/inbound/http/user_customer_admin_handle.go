package userhttp

import (
	"strconv"
	"strings"
	"time"

	"net/http"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/response"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const customerDayLayout = "2006-01-02"

const userCustomerErrScene = "user_customer_write"

type CustomerHandle struct {
	svc usercontract.CustomerAdminPort
}

func NewCustomerHandle(svc usercontract.CustomerAdminPort) *CustomerHandle {
	return &CustomerHandle{svc: svc}
}

func localizeCustomerLabels(c *gin.Context, resps ...*userdto.CustomerResp) {
	for _, r := range resps {
		if r == nil {
			continue
		}
		r.StatusLabel = customerStatusText(c, r.Status)
	}
}

func localizeCustomerStatusLabel(c *gin.Context, res *userdto.CustomerStatusResp) {
	if res == nil {
		return
	}
	res.StatusLabel = customerStatusText(c, res.Status)
}

func (h *CustomerHandle) ListCustomers(c *gin.Context) {
	req := &userdto.CustomerListReq{
		Keyword:       strings.TrimSpace(c.Query("keyword")),
		Status:        customerQueryStatus(c.Query("status")),
		EmailVerified: customerQueryEmailVerified(c.Query("emailVerified")),
		Offset:        customerQueryInt(c.Query("offset"), 0),
		Limit:         customerQueryInt(c.Query("limit"), 0),
	}
	from, ferr := customerDayStart(c.Query("registeredFrom"))
	if ferr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, userenums.ErrInvalidParam)
		return
	}
	to, terr := customerDayEnd(c.Query("registeredTo"))
	if terr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, userenums.ErrInvalidParam)
		return
	}
	req.RegisteredFrom, req.RegisteredTo = utils.NewJSONTimePtr(from), utils.NewJSONTimePtr(to)

	res, err := h.svc.ListCustomers(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "user", err)
		return
	}
	if res != nil {
		localizeCustomerLabels(c, res.List...)
	}
	response.Success(c, res)
}

func (h *CustomerHandle) GetCustomer(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("customerId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, userenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetCustomer(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "user", err)
		return
	}
	if res != nil {
		localizeCustomerLabels(c, res)
	}
	response.Success(c, res)
}

// SetCustomerStatus 接收标准表单：客户与目标状态都来自页面控件，结果由 Jet 返回。
func (h *CustomerHandle) SetCustomerStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form userdto.CustomerStatusForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, userCustomerErrScene, userenums.Err(userenums.ErrInvalidParam))
		return
	}
	id, status, err := form.CustomerStatusValues()
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, userCustomerErrScene, err)
		return
	}
	res, err := h.svc.SetCustomerStatus(c.Request.Context(), &userdto.CustomerStatusReq{CustomerID: id, Status: status})
	if err != nil {
		pageview.FailScene(c, userCustomerErrScene, err)
		return
	}
	msgKey := userenums.MsgCustomerDisabled
	if res.Status == userenums.StatusActive {
		msgKey = userenums.MsgCustomerEnabled
	}
	customerWriteResult(c, msgKey)
}

func (h *CustomerHandle) UnlockCustomer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form userdto.CustomerUnlockForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, userCustomerErrScene, userenums.Err(userenums.ErrInvalidParam))
		return
	}
	id, err := form.CustomerUnlockValue()
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, userCustomerErrScene, err)
		return
	}
	res, err := h.svc.UnlockCustomer(c.Request.Context(), &userdto.CustomerUnlockReq{CustomerID: id})
	if err != nil {
		pageview.FailScene(c, userCustomerErrScene, err)
		return
	}
	msgKey := userenums.MsgCustomerNotLocked
	switch {
	case res.Unlocked:
		msgKey = userenums.MsgCustomerUnlocked
	case res.Cleared:
		msgKey = userenums.MsgCustomerFailuresCleared
	}
	customerWriteResult(c, msgKey)
}

func customerQueryStatus(raw string) int {
	v := strings.TrimSpace(raw)
	if v == "" {
		return userdto.CustomerStatusAll
	}
	switch v {
	case "1":
		return 1
	case "0":
		return 0
	case "2":
		return 2
	default:
		return userdto.CustomerStatusAll
	}
}

func customerQueryEmailVerified(raw string) int {
	switch strings.TrimSpace(raw) {
	case "1":
		return userdto.EmailVerifiedYes
	case "2":
		return userdto.EmailVerifiedNo
	default:
		return userdto.EmailVerifiedAll
	}
}

func customerQueryInt(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return fallback
	}
	return v
}

func customerDayStart(raw string) (*time.Time, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil, nil
	}

	day, err := time.ParseInLocation(customerDayLayout, v, sitetz.Location())
	if err != nil {
		return nil, err
	}
	return &day, nil
}

func customerDayEnd(raw string) (*time.Time, error) {
	day, err := customerDayStart(raw)
	if err != nil || day == nil {
		return nil, err
	}
	end := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return &end, nil
}
