package adminhttp

import (
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/auth"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle admin 模块统一 HTTP 处理器。
// 持有按领域拆分的契约接口，便于 mock 测试与多实现替换。
type Handle struct {
	admin admincontract.AdminService
	role  admincontract.RoleService
	perm  admincontract.PermService
	menu  admincontract.MenuService
	dept  admincontract.DeptService
	rule  admincontract.RuleService
}

// NewHandle 创建 admin HTTP 处理器，注入合并后的 Service 实现。
func NewHandle(svc *adminservice.Service) *Handle {
	return &Handle{
		admin: svc,
		role:  svc,
		perm:  svc,
		menu:  svc,
		dept:  svc,
		rule:  svc,
	}
}

// NewHandleWithDeps 显式注入各领域契约（测试 mock 与特殊装配用）。
func NewHandleWithDeps(
	admin admincontract.AdminService,
	role admincontract.RoleService,
	perm admincontract.PermService,
	menu admincontract.MenuService,
	dept admincontract.DeptService,
	rule admincontract.RuleService,
) *Handle {
	return &Handle{admin: admin, role: role, perm: perm, menu: menu, dept: dept, rule: rule}
}

// AdminList 管理员分页列表。
func (h *Handle) AdminList(c *gin.Context) {
	var req admindto.AdminListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+":"+err.Error())
		return
	}

	res, err := h.admin.AdminList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

// AdminLogin 管理员登录
func (h *Handle) AdminLogin(c *gin.Context) {
	var req admindto.AdminLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}

	res, err := h.admin.AdminLogin(c.Request.Context(), &req, c.ClientIP())
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}

	// 写 cookie session（认证载体；HTMX 请求自动携带 Cookie，无需前端手动带 Authorization 头）
	if err := auth.SaveCookieSession(c, &auth.CookieSession{
		UserID:    res.UserID,
		Username:  res.Username,
		SessionID: res.SessionID,
		IssuedAt:  res.IssuedAt,
	}, res.RememberMe); err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	// 登录即生成 CSRF token，供后续 POST 写操作校验；
	// token 必须随响应 data 返回，否则客户端拿不到 token，后台所有 POST 会被 CSRF 中间件 403
	csrfToken, err := builtin.EnsureCSRFToken(c)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.SuccessWithMessage(c, adminenums.MsgSuccess, gin.H{"csrf_token": csrfToken})
}

// AdminLogout 注销当前登录会话。
func (h *Handle) AdminLogout(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	uid, ok := userID.(int64)
	if !ok {
		r.ErrorWithMessage(c, 500, adminenums.MsgWrongUserType)
		return
	}
	if err := h.admin.AdminLogout(c.Request.Context(), uint64(uid)); err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	// 清空 cookie session（Redis 会话已在 service 层删除）
	if err := auth.ClearSession(c); err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	c.Set(auth.ContextSessionRevokedKey, true)
	r.SuccessWithMessage(c, adminenums.MsgLogoutSuccess, nil)
}

// AdminProfile 获取当前登录用户信息。
func (h *Handle) AdminProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}

	uid, ok := userID.(int64)
	if !ok {
		r.ErrorWithMessage(c, 500, adminenums.MsgWrongUserType)
		return
	}

	res, err := h.admin.AdminProfile(c.Request.Context(), uint64(uid))
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

// AdminCreate 新增管理员
func (h *Handle) AdminCreate(c *gin.Context) {
	var req admindto.AdminCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}
	res, err := h.admin.AdminCreate(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// AdminEdit 修改管理员
func (h *Handle) AdminEdit(c *gin.Context) {
	var req admindto.AdminEditReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+":"+err.Error())
		return
	}
	res, err := h.admin.AdminEdit(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// AdminDetail 管理员详情
func (h *Handle) AdminDetail(c *gin.Context) {
	var req admindto.AdminDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}

	res, err := h.admin.AdminDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

// AdminDelete 删除管理员
func (h *Handle) AdminDelete(c *gin.Context) {
	var req admindto.AdminDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+"："+err.Error())
		return
	}

	userID, exists := c.Get("user_id")
	uid, ok := userID.(int64)
	if !exists || !ok || uid <= 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uint64(uid)

	res, err := h.admin.AdminDelete(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}

	r.Success(c, res)
}

// AdminRoleList 查询用户绑定的角色列表。
func (h *Handle) AdminRoleList(c *gin.Context) {
	var req admindto.AdminRoleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.admin.AdminRoleList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// AdminRoleSave 保存用户角色绑定。
func (h *Handle) AdminRoleSave(c *gin.Context) {
	var req admindto.AdminRoleSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.admin.AdminRoleSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// AdminMenuList 查询用户直接额外菜单。
func (h *Handle) AdminMenuList(c *gin.Context) {
	var req admindto.AdminMenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.admin.AdminMenuList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// AdminMenuSave 保存用户直接额外权限。
func (h *Handle) AdminMenuSave(c *gin.Context) {
	var req admindto.AdminMenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.admin.AdminMenuSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// AdminRoutes 动态路由权限投影。
func (h *Handle) AdminRoutes(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	uid, ok := userID.(int64)
	if !ok {
		r.ErrorWithMessage(c, 500, adminenums.MsgWrongUserType)
		return
	}
	res, err := h.admin.AdminRoutes(c.Request.Context(), uint64(uid), r.RequestLanguage(c))
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}
