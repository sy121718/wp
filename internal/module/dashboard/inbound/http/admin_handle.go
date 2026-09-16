package dashboardhttp

import (
	"net/http"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"

	"github.com/gin-gonic/gin"
)

// Package dashboardhttp 的 admin 六领域管理页（Admin → administrators）。

//

// 管理员/角色/权限点/菜单/部门/数据权限的 CRUD API 与 service 在 admin 模块就绪，

// 本文件在 dashboard 模块补全对应用户后台页面：列表/树展示 + 新建/编辑/删除。

// 严格跨模块：dashboard 只依赖 admin 的 contract 与不可变 DTO，不 import service/model；

// 枚举/响应消息走 dashboard enums 与 admin 模块 enums（service 返回的 err 即模块枚举 key）。

//

// 交互遵循后台规范：普通表单 POST + 303 重定向整页刷新；只用 GET/POST，无 RESTful 路径参数。

// AdministratorsPage 管理员列表页（GET /admin/administrators）。
// 分页列表 + 行内编辑/删除；新建走顶部表单。
func (h *Handle) AdministratorsPage(c *gin.Context) {
	res, err := h.admins.AdminList(c.Request.Context(), &admindto.AdminListReq{
		AdminPageReq: admindto.AdminPageReq{Page: 1, Limit: 100},
	})
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgAdminGenericFailed)
		return
	}
	c.HTML(http.StatusOK, "admin/administrators", withCSRF(c, gin.H{
		"title": dashboardenums.MsgAdministratorsTitle,
		"menu":  "admins",
		"Rows":  res.List,
		"Total": res.Total,
	}))
}

// AdministratorsCreate 新建管理员（POST /admin/administrators/create）。
// service 强制 status=启用；必填 username/email/password(≥6)。
func (h *Handle) AdministratorsCreate(c *gin.Context) {
	username := fieldValue(c, "username")
	email := fieldValue(c, "email")
	password := fieldValue(c, "password")
	if username == "" || email == "" || password == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if _, err := h.admins.AdminCreate(c.Request.Context(), &admindto.AdminCreateReq{
		Username: username, Email: email, Password: password,
		Phone: fieldValue(c, "phone"), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// AdministratorsUpdate 编辑管理员（POST /admin/administrators/update）。
// AdminEdit 仅更新 username/phone/email/remark。
func (h *Handle) AdministratorsUpdate(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if _, err := h.admins.AdminEdit(c.Request.Context(), &admindto.AdminEditReq{
		Id: id, Username: fieldValue(c, "username"), Phone: fieldValue(c, "phone"),
		Email: fieldValue(c, "email"), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// AdministratorsDelete 删除管理员（POST /admin/administrators/delete）。
func (h *Handle) AdministratorsDelete(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{Id: []uint64{id}}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// --- 角色 roles ---

// RolesPage 角色列表页（GET /admin/roles）。
func (h *Handle) RolesPage(c *gin.Context) {
	res, err := h.roles.RoleList(c.Request.Context(), &admindto.RoleListReq{RolePageReq: admindto.RolePageReq{Page: 1, Limit: 100}})
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgAdminGenericFailed)
		return
	}
	c.HTML(http.StatusOK, "admin/roles", withCSRF(c, gin.H{
		"title": dashboardenums.MsgRolesTitle,
		"menu":  "roles",
		"Rows":  res.List,
		"Total": res.Total,
	}))
}

// RolesCreate 新建角色（POST /admin/roles/create）。
func (h *Handle) RolesCreate(c *gin.Context) {
	roleCode := fieldValue(c, "role_code")
	roleName := fieldValue(c, "role_name")
	if roleCode == "" || roleName == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(fieldValue(c, "sort_order"))
	if err := h.roles.RoleCreate(c.Request.Context(), &admindto.RoleCreateReq{
		RoleCode: roleCode, RoleName: roleName, Status: parseStatusPtr(c.PostForm("status")),
		SortOrder: sortOrder, Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesUpdate 编辑角色（POST /admin/roles/update）。
func (h *Handle) RolesUpdate(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	roleName := fieldValue(c, "role_name")
	if id == 0 || roleName == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(fieldValue(c, "sort_order"))
	if err := h.roles.RoleUpdate(c.Request.Context(), &admindto.RoleUpdateReq{
		ID: id, RoleName: roleName, Status: parseStatus(c.PostForm("status")),
		SortOrder: sortOrder, Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesDelete 删除角色（POST /admin/roles/delete）。
func (h *Handle) RolesDelete(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// --- 权限点 permissions ---
