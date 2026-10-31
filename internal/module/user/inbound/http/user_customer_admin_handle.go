package userhttp

import (
	"strconv"
	"strings"
	"time"

	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

const customerDayLayout = "2006-01-02"

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

func (h *CustomerHandle) SetCustomerStatus(c *gin.Context) {
	body, ok := binding.JSON[userdto.CustomerStatusSetReq](c)
	if !ok {
		return
	}
	if body.Status == nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, userenums.ErrInvalidParam)
		return
	}
	req := &userdto.CustomerStatusReq{CustomerID: body.CustomerID, Status: *body.Status}
	if req.CustomerID == 0 || (req.Status != userenums.StatusActive && req.Status != userenums.StatusDisabled) {
		response.ErrorWithMessage(c, http.StatusBadRequest, userenums.ErrCustomerStatusInvalid)
		return
	}
	res, err := h.svc.SetCustomerStatus(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "user", err)
		return
	}

	localizeCustomerStatusLabel(c, res)
	msg := userenums.MsgCustomerDisabled
	if res.Status == 1 {
		msg = userenums.MsgCustomerEnabled
	}
	response.SuccessWithMessage(c, customerNotice(c, msg), res)
}

func (h *CustomerHandle) UnlockCustomer(c *gin.Context) {
	req, ok := binding.JSON[userdto.CustomerUnlockReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UnlockCustomer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "user", err)
		return
	}

	msg := userenums.MsgCustomerNotLocked
	switch {
	case res.Unlocked:
		msg = userenums.MsgCustomerUnlocked
	case res.Cleared:
		msg = userenums.MsgCustomerFailuresCleared
	}
	response.SuccessWithMessage(c, customerNotice(c, msg), res)
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
