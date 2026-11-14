package adminhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/auth"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"
	"go_wp/pkg/view"
)

type Handle struct {
	admin admincontract.AdminService
	role  admincontract.RoleService
	perm  admincontract.PermService
	menu  admincontract.MenuService
	dept  admincontract.DeptService
	rule  admincontract.RuleService
}

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

func (h *Handle) AdminList(c *gin.Context) {
	req, ok := binding.Query[admindto.AdminListReq](c)
	if !ok {
		return
	}

	res, err := h.admin.AdminList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}

	r.Success(c, res)
}

func (h *Handle) AdminLogin(c *gin.Context) {
	// 登录端点的真实消费者只有登录页 fetch 与自动化脚本，两者都发 application/json。
	// 输入与输出协议分开：输入是原生 JSON body 绑定（失败按 400 参数事实返回），
	// 输出仍是既有 JSON 响应；不再按 Content-Type 在 JSON/表单之间自动选择。
	var req admindto.AdminLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.admin.AdminLogin(c.Request.Context(), &req, c.ClientIP())
	if err != nil {
		adminFail(c, err)
		return
	}

	if err := auth.SaveCookieSession(c, &auth.CookieSession{
		SessionID: res.SessionID,
	}, res.RememberMe); err != nil {
		adminFail(c, err)
		return
	}

	csrfToken, err := builtin.RotateCSRFToken(c)
	if err != nil {
		adminFail(c, err)
		return
	}

	r.SuccessWithMessage(c, adminenums.MsgSuccess, gin.H{"csrf_token": csrfToken})
}

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
		adminFail(c, err)
		return
	}

	if cs, err := auth.GetCookieSession(c); err == nil && cs != nil {
		if err := auth.DeleteSessionIndex(c.Request.Context(), cs.SessionID); err != nil {
			logger.Scene("admin").With("err", err).Warn("清理会话句柄索引失败（不影响登出）")
		}
	}

	if err := auth.ClearSession(c); err != nil {
		adminFail(c, err)
		return
	}

	c.Set(auth.ContextSessionRevokedKey, true)
	r.SuccessWithMessage(c, adminenums.MsgLogoutSuccess, nil)
}

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
		adminFail(c, err)
		return
	}

	r.Success(c, res)
}

func (h *Handle) AdminCreate(c *gin.Context) {
	var req admindto.AdminCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	res, err := h.admin.AdminCreate(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) AdminEdit(c *gin.Context) {
	var req admindto.AdminEditReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	res, err := h.admin.AdminEdit(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) AdminDetail(c *gin.Context) {
	req, ok := binding.Query[admindto.AdminDetailReq](c)
	if !ok {
		return
	}

	res, err := h.admin.AdminDetail(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}

	r.Success(c, res)
}

func (h *Handle) AdminDelete(c *gin.Context) {
	var req admindto.AdminDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	uid := builtin.CurrentUserID(c)
	if uid == 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uid

	res, err := h.admin.AdminDelete(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}

	r.Success(c, res)
}

func (h *Handle) AdminRoleList(c *gin.Context) {
	req, ok := binding.Query[admindto.AdminRoleListReq](c)
	if !ok {
		return
	}
	res, err := h.admin.AdminRoleList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// AdminRoleSave 保存管理员角色，返回 Jet 成功或错误片段。
func (h *Handle) AdminRoleSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.AdminRoleSaveReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		view.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	if req.OperatorID == 0 {
		view.FailStatus(c, http.StatusUnauthorized, adminErrScene, adminenums.Err(adminenums.MsgUnauthorized))
		return
	}
	if _, err := h.admin.AdminRoleSave(c.Request.Context(), &req); err != nil {
		view.FailScene(c, "admin.write", err)
		return
	}
	view.Success(c)
}

func (h *Handle) AdminMenuList(c *gin.Context) {
	req, ok := binding.Query[admindto.AdminMenuListReq](c)
	if !ok {
		return
	}
	res, err := h.admin.AdminMenuList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// AdminMenuSave 保存管理员直接菜单授权，返回 Jet 片段。
func (h *Handle) AdminMenuSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.AdminMenuSaveReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		view.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	if req.OperatorID == 0 {
		view.FailStatus(c, http.StatusUnauthorized, adminErrScene, adminenums.Err(adminenums.MsgUnauthorized))
		return
	}
	if _, err := h.admin.AdminMenuSave(c.Request.Context(), &req); err != nil {
		view.FailScene(c, "admin.write", err)
		return
	}
	view.Success(c)
}

func (h *Handle) RuleList(c *gin.Context) {
	req, ok := binding.Query[admindto.RuleListReq](c)
	if !ok {
		return
	}
	res, err := h.rule.RuleList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RuleDetail(c *gin.Context) {
	req, ok := binding.Query[admindto.RuleDetailReq](c)
	if !ok {
		return
	}
	res, err := h.rule.RuleDetail(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RuleCreate(c *gin.Context) {
	var req admindto.RuleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.rule.RuleCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) RuleUpdate(c *gin.Context) {
	var req admindto.RuleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.rule.RuleUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) RuleDelete(c *gin.Context) {
	var req admindto.RuleDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.rule.RuleDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) RuleSchemaList(c *gin.Context) {
	res, err := h.rule.RuleSchemaList(c.Request.Context())
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RuleSchemaDetail(c *gin.Context) {
	req, ok := binding.Query[admindto.RuleSchemaDetailReq](c)
	if !ok {
		return
	}
	res, err := h.rule.RuleSchemaDetail(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	if res == nil {
		r.ErrorWithMessage(c, 400, adminenums.ErrInvalidDomain)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RuleAssignmentList(c *gin.Context) {
	req, ok := binding.Query[admindto.RuleAssignmentListReq](c)
	if !ok {
		return
	}
	res, err := h.rule.RuleAssignmentList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RuleAssignmentSave(c *gin.Context) {
	var req admindto.RuleAssignmentSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.rule.RuleAssignmentSave(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) DeptTree(c *gin.Context) {
	list, err := h.dept.DeptTree(c.Request.Context())
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, list)
}

func (h *Handle) DeptDetail(c *gin.Context) {
	req, ok := binding.Query[admindto.DeptDetailReq](c)
	if !ok {
		return
	}
	res, err := h.dept.DeptDetail(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) DeptCreate(c *gin.Context) {
	var req admindto.DeptCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.dept.DeptCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) DeptUpdate(c *gin.Context) {
	var req admindto.DeptUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.dept.DeptUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) DeptDelete(c *gin.Context) {
	var req admindto.DeptDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.dept.DeptDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) DeptUserList(c *gin.Context) {
	req, ok := binding.Query[admindto.DeptUserListReq](c)
	if !ok {
		return
	}
	res, err := h.dept.DeptUserList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) DeptUserSave(c *gin.Context) {
	var req admindto.DeptUserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.dept.DeptUserSave(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) MenuTree(c *gin.Context) {
	req, ok := binding.Query[admindto.MenuTreeReq](c)
	if !ok {
		return
	}

	list, err := h.menu.MenuTree(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, list)
}

func (h *Handle) MenuDetail(c *gin.Context) {
	req, ok := binding.Query[admindto.MenuDetailReq](c)
	if !ok {
		return
	}

	res, err := h.menu.MenuDetail(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) MenuCreate(c *gin.Context) {
	var req admindto.MenuCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	if err := h.menu.MenuCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) MenuUpdate(c *gin.Context) {
	var req admindto.MenuUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	if err := h.menu.MenuUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) MenuDelete(c *gin.Context) {
	var req admindto.MenuDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	if err := h.menu.MenuDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) PermList(c *gin.Context) {
	req, ok := binding.Query[admindto.PermListReq](c)
	if !ok {
		return
	}

	res, err := h.perm.PermList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) PermDetail(c *gin.Context) {
	req, ok := binding.Query[admindto.PermDetailReq](c)
	if !ok {
		return
	}

	res, err := h.perm.PermDetail(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) PermOptions(c *gin.Context) {
	req, ok := binding.Query[admindto.PermOptionsReq](c)
	if !ok {
		return
	}

	res, err := h.perm.PermOptions(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) PermCreate(c *gin.Context) {
	var req admindto.PermCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	res, err := h.perm.PermCreate(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

func (h *Handle) PermUpdate(c *gin.Context) {
	var req admindto.PermUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	res, err := h.perm.PermUpdate(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

func (h *Handle) PermDelete(c *gin.Context) {
	var req admindto.PermDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	res, err := h.perm.PermDelete(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

func (h *Handle) RoleList(c *gin.Context) {
	req, ok := binding.Query[admindto.RoleListReq](c)
	if !ok {
		return
	}
	res, err := h.role.RoleList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RoleDetail(c *gin.Context) {
	req, ok := binding.Query[admindto.RoleDetailReq](c)
	if !ok {
		return
	}
	res, err := h.role.RoleDetail(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RoleCreate(c *gin.Context) {
	var req admindto.RoleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.role.RoleCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) RoleUpdate(c *gin.Context) {
	var req admindto.RoleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.role.RoleUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) RoleDelete(c *gin.Context) {
	var req admindto.RoleDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}
	if err := h.role.RoleDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

func (h *Handle) RoleMenuList(c *gin.Context) {
	req, ok := binding.Query[admindto.RoleMenuListReq](c)
	if !ok {
		return
	}
	res, err := h.role.RoleMenuList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RoleMenuSave 保存角色菜单授权，返回 Jet 成功或错误片段。
func (h *Handle) RoleMenuSave(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.RoleMenuSaveReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		view.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	if req.OperatorID == 0 {
		view.FailStatus(c, http.StatusUnauthorized, adminErrScene, adminenums.Err(adminenums.MsgUnauthorized))
		return
	}
	if _, err := h.role.RoleMenuSave(c.Request.Context(), &req); err != nil {
		view.FailScene(c, "admin.write", err)
		return
	}
	view.Success(c)
}

func (h *Handle) RoleUserList(c *gin.Context) {
	req, ok := binding.Query[admindto.RoleUserListReq](c)
	if !ok {
		return
	}
	res, err := h.role.RoleUserList(c.Request.Context(), req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

func (h *Handle) RoleUserSave(c *gin.Context) {
	var req admindto.RoleUserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ParamError(c)
		return
	}

	uid := builtin.CurrentUserID(c)
	if uid == 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uid
	res, err := h.role.RoleUserSave(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}
