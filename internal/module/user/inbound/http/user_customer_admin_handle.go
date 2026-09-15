package userhttp

// user_customer_admin_handle.go — 后台客户管理的 JSON API（/api/customer/*）。
//
// 同一个模块的两个面，各挂各的链路：
//
//	/user/*           公开面：访客自己的 cookie 会话 + 自己的 CSRF token，**没有权限点**
//	                  （访客能做的只有操作自己的账号，见包注释）；
//	/api/customer/*   管理面：挂 authorizedAPI 的 Session + CSRF + Casbin 三层链，
//	                  权限点 user:customer_*（迁移 152）。
//
// 路径用 customer 前缀而不是 user：/api/user/* 与公开面的 /user/* 只差一段前缀，
// 翻日志时几乎分不出来，而这两个面的越权含义完全相反。
//
// handler 只做三件事：绑定参数、调 service、输出响应（业务判断一律在 service）。

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"go_wp/pkg/sitetz"

	"github.com/gin-gonic/gin"

	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/response"
)

// customerDayLayout 筛选用的日期格式（与后台页面的 date 输入一致）。
const customerDayLayout = "2006-01-02"

// CustomerHandle 后台客户管理 HTTP 处理器。
type CustomerHandle struct {
	svc usercontract.CustomerAdminPort
}

// NewCustomerHandle 构造。
func NewCustomerHandle(svc usercontract.CustomerAdminPort) *CustomerHandle {
	return &CustomerHandle{svc: svc}
}

// SetupCustomerAdminRoutes 挂载后台客户管理路由（挂 authorizedAPI 组）。
//
// svc 为 nil 时直接不注册：装配缺陷应该由调用方（routes.go 的断言）炸掉，
// 而不是在这里注册一批「一调就 500」的接口。
func SetupCustomerAdminRoutes(rg *gin.RouterGroup, svc usercontract.CustomerAdminPort) {
	if rg == nil || svc == nil {
		return
	}
	h := NewCustomerHandle(svc)
	g := rg.Group("/customer")
	g.GET("/list", h.ListCustomers)
	g.GET("/get", h.GetCustomer)
	g.POST("/status", h.SetCustomerStatus)
	g.POST("/unlock", h.UnlockCustomer)
}

// ListCustomers 客户列表（GET /api/customer/list）。
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
	req.RegisteredFrom, req.RegisteredTo = from, to

	res, err := h.svc.ListCustomers(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "user", err)
		return
	}
	response.Success(c, res)
}

// GetCustomer 客户详情（GET /api/customer/get?customerId=）。
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
	response.Success(c, res)
}

// SetCustomerStatus 启用 / 停用账号（POST /api/customer/status）。
func (h *CustomerHandle) SetCustomerStatus(c *gin.Context) {
	req := &userdto.CustomerStatusReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, userenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SetCustomerStatus(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "user", err)
		return
	}
	// 回执文案按目标状态给：调用方拿到「账号已启用」比拿到一个数字更不容易用错。
	msg := userenums.MsgCustomerDisabled
	if res.Status == 1 {
		msg = userenums.MsgCustomerEnabled
	}
	response.SuccessWithMessage(c, msg, res)
}

// UnlockCustomer 解除登录锁定（POST /api/customer/unlock）。
func (h *CustomerHandle) UnlockCustomer(c *gin.Context) {
	req := &userdto.CustomerUnlockReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, userenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UnlockCustomer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "user", err)
		return
	}
	// 三种结果各说各的：解除了锁定 / 清了残留计数 / 本来就没事。
	msg := userenums.MsgCustomerNotLocked
	switch {
	case res.Unlocked:
		msg = userenums.MsgCustomerUnlocked
	case res.Cleared:
		msg = userenums.MsgCustomerFailuresCleared
	}
	response.SuccessWithMessage(c, msg, res)
}

// —— 参数解析 ——

// customerQueryStatus 解析状态筛选：空串 = 全部（**不能**让空串落成 0 —— 0 是「已停用」，
// 那会让不传 status 的调用方只看到被停用的账号）。
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

// customerQueryEmailVerified 解析邮箱验证筛选：空串 = 全部。
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

// customerQueryInt 解析非负整数，失败或为空落默认值（列表类参数容错即可，
// 不必为一个拼错的 limit 报错 —— 副作用只是回到默认页大小）。
func customerQueryInt(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return fallback
	}
	return v
}

// customerDayStart 日期 → 当天 00:00:00（解析不了即报错，不静默忽略）。
func customerDayStart(raw string) (*time.Time, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil, nil
	}
	// 日期筛选按**站点时区**解释（pkg/sitetz）：运营说的「9 月 14 日」是站点所在地的那一天。
	// 用 time.Local 会让同一次筛选随部署机器给出不同的结果集（审计 TX-011）。
	day, err := time.ParseInLocation(customerDayLayout, v, sitetz.Location())
	if err != nil {
		return nil, err
	}
	return &day, nil
}

// customerDayEnd 日期 → 当天 23:59:59.999999999。
//
// 必须扩到当天最后一刻：把结束日期当成 00:00:00，那一天注册的客户**一个都筛不出来**，
// 而运营以为自己筛的是「到 9 月 30 日为止」—— 少一天的结果看起来完全正常。
func customerDayEnd(raw string) (*time.Time, error) {
	day, err := customerDayStart(raw)
	if err != nil || day == nil {
		return nil, err
	}
	end := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return &end, nil
}
