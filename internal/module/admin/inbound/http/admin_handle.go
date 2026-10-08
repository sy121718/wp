package adminhttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/admin/contract"
	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/service"
	"go_wp/internal/shell"
	"go_wp/pkg/auth"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"
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
		adminBindFail(c, err)
		return
	}

	res, err := h.admin.AdminList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}

	r.Success(c, res)
}

// AdminLogin 管理员登录
func (h *Handle) AdminLogin(c *gin.Context) {
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

	// 写 cookie session：**只写会话句柄**（P1 句柄化）。
	// 身份与 issued_at 都在 Redis（SaveUserSession 已连同索引一并写入），
	// cookie 里只有那个随机串 —— 客户端读不出也声明不了自己是谁。
	// HTMX 请求自动携带 Cookie，无需前端手动带 Authorization 头。
	if err := auth.SaveCookieSession(c, &auth.CookieSession{
		SessionID: res.SessionID,
	}, res.RememberMe); err != nil {
		adminFail(c, err)
		return
	}

	// 登录成功强制轮换 CSRF token（不复用匿名期 token，防预置 cookie 攻击），
	// 供后续 POST 写操作校验；token 必须随响应 data 返回，否则客户端拿不到
	// token，后台所有 POST 会被 CSRF 中间件 403。
	csrfToken, err := builtin.RotateCSRFToken(c)
	if err != nil {
		adminFail(c, err)
		return
	}

	r.SuccessWithMessage(c, adminenums.MsgSuccess, gin.H{"csrf_token": csrfToken})
}

// AdminLogout 注销当前登录会话。
//
// 这里取的是「当前会话主体」而非审计操作人，且要区分「未登录(401)」与「会话值
// 类型异常(500)」两种失败 —— shell.CurrentUserID 会把两者都压成 0，故不换它。
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

	// 顺手清掉句柄索引（P1 句柄化）：service 层按 userID 删了会话明细，而索引是按
	// 句柄建的、那条路径拿不到句柄。不清只是脏数据（明细没了照样拒绝认证），
	// 但这里正好有 cookie，清掉可以让索引表不随登出次数膨胀。
	if cs, err := auth.GetCookieSession(c); err == nil && cs != nil {
		if err := auth.DeleteSessionIndex(c.Request.Context(), cs.SessionID); err != nil {
			logger.Scene("admin").With("err", err).Warn("清理会话句柄索引失败（不影响登出）")
		}
	}

	// 清空 cookie session（Redis 会话已在 service 层删除）
	if err := auth.ClearSession(c); err != nil {
		adminFail(c, err)
		return
	}

	c.Set(auth.ContextSessionRevokedKey, true)
	r.SuccessWithMessage(c, adminenums.MsgLogoutSuccess, nil)
}

// AdminProfile 获取当前登录用户信息。
//
// 同 AdminLogout：会话主体 + 401/500 两种失败要分开，不走 shell 入口。
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

// AdminCreate 新增管理员
func (h *Handle) AdminCreate(c *gin.Context) {
	var req admindto.AdminCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.admin.AdminCreate(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// AdminEdit 修改管理员
func (h *Handle) AdminEdit(c *gin.Context) {
	var req admindto.AdminEditReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.admin.AdminEdit(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// AdminDetail 管理员详情
func (h *Handle) AdminDetail(c *gin.Context) {
	var req admindto.AdminDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.admin.AdminDetail(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}

	r.Success(c, res)
}

// AdminDelete 删除管理员
func (h *Handle) AdminDelete(c *gin.Context) {
	var req admindto.AdminDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	// 操作人取统一入口（会话注入，禁止前端伪造）；取不到即视为未登录。
	uid := shell.CurrentUserID(c)
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

// AdminRoleList 查询用户绑定的角色列表。
func (h *Handle) AdminRoleList(c *gin.Context) {
	var req admindto.AdminRoleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.admin.AdminRoleList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// AdminRoleSave 保存用户角色绑定。
func (h *Handle) AdminRoleSave(c *gin.Context) {
	var req admindto.AdminRoleSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	// 注入当前操作者（超管保护判定依据，禁止前端伪造）；取不到即视为未登录。
	uid := shell.CurrentUserID(c)
	if uid == 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uid
	res, err := h.admin.AdminRoleSave(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// AdminMenuList 查询用户直接额外菜单。
func (h *Handle) AdminMenuList(c *gin.Context) {
	var req admindto.AdminMenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.admin.AdminMenuList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// AdminMenuSave 保存用户直接额外权限。
func (h *Handle) AdminMenuSave(c *gin.Context) {
	var req admindto.AdminMenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	// 注入当前操作者（超管保护判定依据，禁止前端伪造）；取不到即视为未登录。
	uid := shell.CurrentUserID(c)
	if uid == 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uid
	res, err := h.admin.AdminMenuSave(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// RuleList 数据规则分页列表。
func (h *Handle) RuleList(c *gin.Context) {
	var req admindto.RuleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.rule.RuleList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RuleDetail 数据规则详情。
func (h *Handle) RuleDetail(c *gin.Context) {
	var req admindto.RuleDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.rule.RuleDetail(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RuleCreate 新建数据规则。
func (h *Handle) RuleCreate(c *gin.Context) {
	var req admindto.RuleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.rule.RuleCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RuleUpdate 更新数据规则。
func (h *Handle) RuleUpdate(c *gin.Context) {
	var req admindto.RuleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.rule.RuleUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RuleDelete 批量删除数据规则。
func (h *Handle) RuleDelete(c *gin.Context) {
	var req admindto.RuleDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.rule.RuleDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RuleSchemaList 返回所有已注册 domain。
func (h *Handle) RuleSchemaList(c *gin.Context) {
	res, err := h.rule.RuleSchemaList(c.Request.Context())
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RuleSchemaDetail 返回 domain 的字段白名单详情。
func (h *Handle) RuleSchemaDetail(c *gin.Context) {
	var req admindto.RuleSchemaDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.rule.RuleSchemaDetail(c.Request.Context(), &req)
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

// RuleAssignmentList 查询规则分配列表。
func (h *Handle) RuleAssignmentList(c *gin.Context) {
	var req admindto.RuleAssignmentListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.rule.RuleAssignmentList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RuleAssignmentSave 批量保存规则分配。
func (h *Handle) RuleAssignmentSave(c *gin.Context) {
	var req admindto.RuleAssignmentSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.rule.RuleAssignmentSave(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// DeptTree 获取完整部门树。
func (h *Handle) DeptTree(c *gin.Context) {
	list, err := h.dept.DeptTree(c.Request.Context())
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, list)
}

// DeptDetail 查询部门详情（GET 参数 id）。
func (h *Handle) DeptDetail(c *gin.Context) {
	var req admindto.DeptDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.dept.DeptDetail(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// DeptCreate 新建部门（JSON body）。
func (h *Handle) DeptCreate(c *gin.Context) {
	var req admindto.DeptCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.dept.DeptCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// DeptUpdate 更新部门信息，支持移动父节点（JSON body）。
func (h *Handle) DeptUpdate(c *gin.Context) {
	var req admindto.DeptUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.dept.DeptUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// DeptDelete 删除部门（JSON body id）。
func (h *Handle) DeptDelete(c *gin.Context) {
	var req admindto.DeptDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.dept.DeptDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// DeptUserList 查询部门下的用户列表（GET 参数）。
func (h *Handle) DeptUserList(c *gin.Context) {
	var req admindto.DeptUserListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.dept.DeptUserList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// DeptUserSave 批量分配用户到部门（JSON body）。
func (h *Handle) DeptUserSave(c *gin.Context) {
	var req admindto.DeptUserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.dept.DeptUserSave(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// MenuTree 菜单树查询接口。
func (h *Handle) MenuTree(c *gin.Context) {
	var req admindto.MenuTreeReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	list, err := h.menu.MenuTree(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, list)
}

// MenuDetail 菜单详情接口。
func (h *Handle) MenuDetail(c *gin.Context) {
	var req admindto.MenuDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.menu.MenuDetail(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// MenuCreate 新建菜单接口。
func (h *Handle) MenuCreate(c *gin.Context) {
	var req admindto.MenuCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	if err := h.menu.MenuCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// MenuUpdate 更新菜单接口。
func (h *Handle) MenuUpdate(c *gin.Context) {
	var req admindto.MenuUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	if err := h.menu.MenuUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// MenuDelete 批量删除菜单接口。
func (h *Handle) MenuDelete(c *gin.Context) {
	var req admindto.MenuDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	if err := h.menu.MenuDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// PermList 权限点分页列表接口。
func (h *Handle) PermList(c *gin.Context) {
	var req admindto.PermListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.perm.PermList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// PermDetail 权限点详情接口。
func (h *Handle) PermDetail(c *gin.Context) {
	var req admindto.PermDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.perm.PermDetail(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// PermOptions 启用权限选项接口。
func (h *Handle) PermOptions(c *gin.Context) {
	var req admindto.PermOptionsReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.perm.PermOptions(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// PermCreate 新建权限点接口。
func (h *Handle) PermCreate(c *gin.Context) {
	var req admindto.PermCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.perm.PermCreate(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// PermUpdate 更新权限点接口。
func (h *Handle) PermUpdate(c *gin.Context) {
	var req admindto.PermUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.perm.PermUpdate(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// PermDelete 批量删除权限点接口。
func (h *Handle) PermDelete(c *gin.Context) {
	var req admindto.PermDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}

	res, err := h.perm.PermDelete(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// RoleList 角色分页列表接口。
func (h *Handle) RoleList(c *gin.Context) {
	var req admindto.RoleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.role.RoleList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RoleDetail 角色详情接口。
func (h *Handle) RoleDetail(c *gin.Context) {
	var req admindto.RoleDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.role.RoleDetail(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RoleCreate 新建角色接口。
func (h *Handle) RoleCreate(c *gin.Context) {
	var req admindto.RoleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.role.RoleCreate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RoleUpdate 更新角色接口。
func (h *Handle) RoleUpdate(c *gin.Context) {
	var req admindto.RoleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.role.RoleUpdate(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RoleDelete 删除角色接口。
func (h *Handle) RoleDelete(c *gin.Context) {
	var req admindto.RoleDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	if err := h.role.RoleDelete(c.Request.Context(), &req); err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RoleMenuList 角色拥有的菜单 ID 列表接口。
func (h *Handle) RoleMenuList(c *gin.Context) {
	var req admindto.RoleMenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.role.RoleMenuList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RoleMenuSave 保存角色菜单授权接口。
func (h *Handle) RoleMenuSave(c *gin.Context) {
	var req admindto.RoleMenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	// 注入当前操作者（超管保护判定依据，禁止前端伪造）；取不到即视为未登录。
	uid := shell.CurrentUserID(c)
	if uid == 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uid
	res, err := h.role.RoleMenuSave(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// RoleUserList 角色下的用户列表接口。
func (h *Handle) RoleUserList(c *gin.Context) {
	var req admindto.RoleUserListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	res, err := h.role.RoleUserList(c.Request.Context(), &req)
	if err != nil {
		adminFail(c, err)
		return
	}
	r.Success(c, res)
}

// RoleUserSave 保存角色用户绑定接口。
func (h *Handle) RoleUserSave(c *gin.Context) {
	var req admindto.RoleUserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		adminBindFail(c, err)
		return
	}
	// 注入当前操作者（超管保护判定依据，禁止前端伪造）；取不到即视为未登录。
	uid := shell.CurrentUserID(c)
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
