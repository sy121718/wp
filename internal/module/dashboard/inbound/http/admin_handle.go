// Package dashboardhttp 的 admin 六领域管理页（Admin → administrators）。
//
// 管理员/角色/权限点/菜单/部门/数据权限的 CRUD API 与 service 在 admin 模块就绪，
// 本文件在 dashboard 模块补全对应用户后台页面：列表/树展示 + 新建/编辑/删除。
// 严格跨模块：dashboard 只依赖 admin 的 contract 与不可变 DTO，不 import service/model；
// 枚举/响应消息走 dashboard enums 与 admin 模块 enums（service 返回的 err 即模块枚举 key）。
//
// 交互遵循后台规范：普通表单 POST + 303 重定向整页刷新；只用 GET/POST，无 RESTful 路径参数。

package dashboardhttp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	"go_wp/pkg/datarule"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// ---- 通用 helper ----

// fieldValue 取 PostForm 值并去掉首尾空白；无值返回空串。
func fieldValue(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

// parseUint 解析非负整数；空串或非法返回 0。
func parseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

// parseStatus 解析状态；空串返回 0（多数域 0=禁用，service 默认启用由各 create 处理）。
func parseStatus(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

// parseStatusPtr 解析状态为 *int（RoleCreate 用 nil 表示未传即启用）。
func parseStatusPtr(s string) *int {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	v, _ := strconv.Atoi(t)
	return &v
}

// adminWriteFailed 统一的页面写操作失败响应：直接透出 service 返回的模块枚举错误消息，
// 不经由内部细节；必填缺失返回 BadRequest，其余按 422 处理。
func adminWriteFailed(c *gin.Context, err error) {
	if err == nil {
		return
	}
	logger.Scene("admin-page").With("path", c.Request.URL.Path).Error(err, "管理页写操作失败")
	c.String(http.StatusBadRequest, err.Error())
}

// --- 管理员 administrators ---

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

// PermissionsPage 权限资源列表页（GET /admin/permissions）。
func (h *Handle) PermissionsPage(c *gin.Context) {
	page, limit := pageParams(c)
	// 服务端筛选：筛选条件进 SQL（与分页同源），避免「前端只过滤当前页」的错配。
	code := strings.TrimSpace(c.Query("code"))
	module := strings.TrimSpace(c.Query("module"))
	res, err := h.perms.PermList(c.Request.Context(), &admindto.PermListReq{
		PermPageReq: admindto.PermPageReq{Page: page, Limit: limit},
		Code:        code,
		Module:      module,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgAdminGenericFailed)
		return
	}
	data := withCSRF(c, gin.H{
		"title":        dashboardenums.MsgPermissionsTitle,
		"menu":         "permissions",
		"Rows":         res.List,
		"Total":        res.Total,
		"FilterCode":   code,
		"FilterModule": module,
	})
	base := filterBaseURL("/admin/permissions", map[string]string{"code": code, "module": module})
	for k, v := range buildPagination(res.Total, page, limit, base, translateFor(c)).templateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/permissions", data)
}

// filterBaseURL 拼出「路径 + 非空筛选参数」作为分页链接前缀，翻页时保留筛选条件。
func filterBaseURL(path string, filters map[string]string) string {
	q := url.Values{}
	for k, v := range filters {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// pageParams 读取分页查询参数（?page=&limit=），缺省第 1 页、每页 20 条。
// limit 上限 100（与各模块 GetLimit() 的上限一致）。
func pageParams(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.Query("page"))
	limit, _ = strconv.Atoi(c.Query("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// PermissionsCreate 新建权限点（POST /admin/permissions/create）。
// permission_code 创建后不可修改；api_method 限 GET/POST。
func (h *Handle) PermissionsCreate(c *gin.Context) {
	code := fieldValue(c, "permission_code")
	name := fieldValue(c, "permission_name")
	module := fieldValue(c, "module")
	apiPath := fieldValue(c, "api_path")
	apiMethod := strings.ToUpper(fieldValue(c, "api_method"))
	if code == "" || name == "" || module == "" || apiPath == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if apiMethod != "GET" && apiMethod != "POST" {
		apiMethod = "POST"
	}
	if _, err := h.perms.PermCreate(c.Request.Context(), &admindto.PermCreateReq{
		PermissionCode: code, PermissionName: name, Module: module,
		APIPath: apiPath, APIMethod: apiMethod, Status: parseStatus(c.PostForm("status")),
		Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/permissions")
}

// PermissionsUpdate 编辑权限点（POST /admin/permissions/update）。
func (h *Handle) PermissionsUpdate(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	name := fieldValue(c, "permission_name")
	module := fieldValue(c, "module")
	apiPath := fieldValue(c, "api_path")
	apiMethod := strings.ToUpper(fieldValue(c, "api_method"))
	if id == 0 || name == "" || module == "" || apiPath == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if apiMethod != "GET" && apiMethod != "POST" {
		apiMethod = "POST"
	}
	if _, err := h.perms.PermUpdate(c.Request.Context(), &admindto.PermUpdateReq{
		ID: id, PermissionCode: fieldValue(c, "permission_code"),
		PermissionName: name, Module: module, APIPath: apiPath, APIMethod: apiMethod,
		Status: parseStatus(c.PostForm("status")), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/permissions")
}

// PermissionsDelete 删除权限点（POST /admin/permissions/delete）。
func (h *Handle) PermissionsDelete(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/permissions")
}

// --- 菜单 menus（树） ---

// menuRow 菜单树展平行。
type menuRow struct {
	ID        uint64
	Title     string
	Path      string
	Type      int
	TypeLabel string
	ParentID  uint64
	Status    int
	SortOrder int
	Remark    string
	Indent    string
	Icon      string
}

// flattenMenuTree 深度优先展平菜单树为带缩进的行（Indent 控制前端层级展示）。
func flattenMenuTree(nodes []admindto.MenuTreeNode, depth int, out *[]menuRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		*out = append(*out, menuRow{
			ID: n.ID, Title: n.Title, Path: n.Path, Type: n.Type, TypeLabel: menuTypeLabel(n.Type),
			ParentID: n.ParentID, Status: n.Status, SortOrder: n.SortOrder,
			Remark: n.Remark, Indent: indent, Icon: n.Icon,
		})
		if len(n.Children) > 0 {
			flattenMenuTree(n.Children, depth+1, out)
		}
	}
}

func menuTypeLabel(t int) string {
	switch t {
	case 1:
		return "目录"
	case 2:
		return "菜单"
	case 3:
		return "按钮"
	case 4:
		return "iframe"
	case 5:
		return "外链"
	default:
		return "未知"
	}
}

// MenusPage 菜单管理页（GET /admin/menus）。
func (h *Handle) MenusPage(c *gin.Context) {
	nodes, err := h.menus.MenuTree(c.Request.Context(), &admindto.MenuTreeReq{})
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgAdminGenericFailed)
		return
	}
	rows := make([]menuRow, 0, 64)
	flattenMenuTree(nodes, 1, &rows)
	// 供新建下拉的父级选项（树扁平行，含全部分级）。
	c.HTML(http.StatusOK, "admin/menus", withCSRF(c, gin.H{
		"title":   dashboardenums.MsgMenusTitle,
		"menu":    "menus",
		"Rows":    rows,
		"Parents": rows,
	}))
}

// MenusCreate 新建菜单（POST /admin/menus/create）。
func (h *Handle) MenusCreate(c *gin.Context) {
	title := fieldValue(c, "title")
	if title == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(fieldValue(c, "sort_order"))
	if err := h.menus.MenuCreate(c.Request.Context(), &admindto.MenuCreateReq{
		Title: title, ParentID: parseUint(c.PostForm("parent_id")),
		Type: parseStatus(c.PostForm("type")), Path: fieldValue(c, "path"),
		Status: parseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// MenusUpdate 编辑菜单（POST /admin/menus/update）。
func (h *Handle) MenusUpdate(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	title := fieldValue(c, "title")
	if id == 0 || title == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(fieldValue(c, "sort_order"))
	if err := h.menus.MenuUpdate(c.Request.Context(), &admindto.MenuUpdateReq{
		ID: id, Title: title, ParentID: parseUint(c.PostForm("parent_id")),
		Type: parseStatus(c.PostForm("type")), Path: fieldValue(c, "path"),
		Status: parseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// MenusDelete 删除菜单（POST /admin/menus/delete）。
func (h *Handle) MenusDelete(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// --- 部门 departments（树） ---

// deptRow 部门树展平行。
type deptRow struct {
	ID        uint64
	DeptName  string
	DeptCode  string
	ParentID  uint64
	Status    int
	SortOrder int
	Remark    string
	Indent    string
}

// flattenDeptTree 深度优先展平部门树为带缩进的行（顶层为值切片，Children 为指针切片）。
func flattenDeptTree(nodes []admindto.DeptTreeNode, depth int, out *[]deptRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		*out = append(*out, deptRow{
			ID: n.ID, DeptName: n.DeptName, DeptCode: n.DeptCode, ParentID: n.ParentID,
			Status: n.Status, SortOrder: n.SortOrder, Remark: n.Remark, Indent: indent,
		})
		if len(n.Children) > 0 {
			flattenDeptNodePtrs(n.Children, depth+1, out)
		}
	}
}

// flattenDeptNodePtrs 递归处理部门子树（Children 为指针切片）。
func flattenDeptNodePtrs(nodes []*admindto.DeptTreeNode, depth int, out *[]deptRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		if n == nil {
			continue
		}
		*out = append(*out, deptRow{
			ID: n.ID, DeptName: n.DeptName, DeptCode: n.DeptCode, ParentID: n.ParentID,
			Status: n.Status, SortOrder: n.SortOrder, Remark: n.Remark, Indent: indent,
		})
		if len(n.Children) > 0 {
			flattenDeptNodePtrs(n.Children, depth+1, out)
		}
	}
}

// DepartmentsPage 部门管理页（GET /admin/departments）。
func (h *Handle) DepartmentsPage(c *gin.Context) {
	nodes, err := h.depts.DeptTree(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgAdminGenericFailed)
		return
	}
	rows := make([]deptRow, 0, 64)
	flattenDeptTree(nodes, 1, &rows)
	c.HTML(http.StatusOK, "admin/departments", withCSRF(c, gin.H{
		"title":   dashboardenums.MsgDepartmentsTitle,
		"menu":    "depts",
		"Rows":    rows,
		"Parents": rows,
	}))
}

// DepartmentsCreate 新建部门（POST /admin/departments/create）。
func (h *Handle) DepartmentsCreate(c *gin.Context) {
	deptName := fieldValue(c, "dept_name")
	deptCode := fieldValue(c, "dept_code")
	if deptName == "" || deptCode == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(fieldValue(c, "sort_order"))
	if err := h.depts.DeptCreate(c.Request.Context(), &admindto.DeptCreateReq{
		ParentID: parseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: parseStatus(c.PostForm("status")), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// DepartmentsUpdate 编辑部门（POST /admin/departments/update）。
func (h *Handle) DepartmentsUpdate(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	deptName := fieldValue(c, "dept_name")
	deptCode := fieldValue(c, "dept_code")
	if id == 0 || deptName == "" || deptCode == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(fieldValue(c, "sort_order"))
	if err := h.depts.DeptUpdate(c.Request.Context(), &admindto.DeptUpdateReq{
		ID: id, ParentID: parseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: parseStatus(c.PostForm("status")), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// DepartmentsDelete 删除部门（POST /admin/departments/delete）。
func (h *Handle) DepartmentsDelete(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// --- 数据权限 datarules ---

// DatarulesPage 数据权限列表页（GET /admin/datarules）。
// 列表 + 新建（domain 下拉来自已注册数据域）+ 删除；编辑走独立 /edit?id= 页（detail 回显，配置复杂）。
func (h *Handle) DatarulesPage(c *gin.Context) {
	res, err := h.rules.RuleList(c.Request.Context(), &admindto.RuleListReq{Page: 1, Limit: 100})
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgAdminGenericFailed)
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	c.HTML(http.StatusOK, "admin/datarules", withCSRF(c, gin.H{
		"title":   dashboardenums.MsgDatarulesTitle,
		"menu":    "datarules",
		"Rows":    res.List,
		"Total":   res.Total,
		"Domains": domains,
	}))
}

// DatarulesCreate 新建数据规则（POST /admin/datarules/create）。
// config 为可选 JSON 文本（RuleConfig；缺省给空配置 {}）。
func (h *Handle) DatarulesCreate(c *gin.Context) {
	ruleName := fieldValue(c, "rule_name")
	domain := fieldValue(c, "domain")
	if ruleName == "" || domain == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	config, err := configFromJSON(fieldValue(c, "config"))
	if err != nil {
		c.String(http.StatusBadRequest, "配置 JSON 不合法")
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: config,
		Status: parseStatus(c.PostForm("status")), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesEditPage 数据规则编辑页（GET /admin/datarules/edit?id=X）。
// 从 RuleDetail 回显 rule_name/domain/status/remark 与 config JSON。
func (h *Handle) DatarulesEditPage(c *gin.Context) {
	id := parseUint(c.Query("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil || detail == nil {
		c.String(http.StatusNotFound, "数据规则不存在")
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	configJSON, err := configToJSON(detail.Config)
	if err != nil {
		configJSON = ""
	}
	c.HTML(http.StatusOK, "admin/datarule_edit", withCSRF(c, gin.H{
		"title":   dashboardenums.MsgDatarulesTitle,
		"menu":    "datarules",
		"Detail":  detail,
		"Domains": domains,
		"Config":  configJSON,
	}))
}

// DatarulesUpdate 保存数据规则（POST /admin/datarules/update）。
// config 为可选字段：列表抽屉表单不含该字段（列表 dto 无 config），此时沿用库中原配置，
// 避免「只改基础字段」把规则配置清空；显式提交 config（含空串）仍按提交值处理。
func (h *Handle) DatarulesUpdate(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	ruleName := fieldValue(c, "rule_name")
	domain := fieldValue(c, "domain")
	if id == 0 || ruleName == "" || domain == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	config, err := configFromJSON(fieldValue(c, "config"))
	if err != nil {
		c.String(http.StatusBadRequest, "配置 JSON 不合法")
		return
	}
	if _, ok := c.GetPostForm("config"); !ok {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil || detail == nil {
			adminWriteFailed(c, detailErr)
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: parseStatus(c.PostForm("status")), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesDelete 删除数据规则（POST /admin/datarules/delete）。
func (h *Handle) DatarulesDelete(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// configToJSON 序列化 RuleConfig 为缩进 JSON 文本（编辑回显）。
func configToJSON(cfg datarule.RuleConfig) (string, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// configFromJSON 解析表单配置 JSON 文本为 RuleConfig；空文本返回空配置。
func configFromJSON(s string) (datarule.RuleConfig, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return datarule.RuleConfig{}, nil
	}
	var cfg datarule.RuleConfig
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		return datarule.RuleConfig{}, err
	}
	return cfg, nil
}
