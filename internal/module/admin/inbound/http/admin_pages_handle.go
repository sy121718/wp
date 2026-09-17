package adminhttp

// admin_pages_handle.go — admin 六领域管理页 + 文案词条页 + 语言切换 + 登录页数据准备。
//
// 自 dashboard 模块搬回 admin 模块（页面住在哪个模块就归哪个模块）：
// 管理员/角色/权限点/菜单/部门/数据权限的管理页此前长期挂在 dashboard 的共享 Handle 上，
// 现在拆成独立结构体，只持有本页面需要的契约。
//
// 严格跨模块边界在此消失：这些页面本来管理的就是 admin 自己的领域，
// 直接持有 service 契约（同包语义），不再绕 dashboard。
// 页面文案标题沿用原有 i18n 词条 key（与 dashboard 副本逐字一致，保证渲染不变）。

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/config"
	"go_wp/internal/web/shell"
	"go_wp/pkg/captcha"
	"go_wp/pkg/datarule"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"

	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
)

// 页面标题与统一提示（沿用原 i18n 词条 key，词条缺失时前端回退中文注释值）。
const (
	pagesMsgAdministratorsTitle = "MsgAdministratorsTitle" // 管理员
	pagesMsgRolesTitle          = "MsgRolesTitle"          // 角色管理
	pagesMsgMenusTitle          = "MsgMenusTitle"          // 菜单管理
	pagesMsgPermissionsTitle    = "MsgPermissionsTitle"    // 权限资源
	pagesMsgDepartmentsTitle    = "MsgDepartmentsTitle"    // 部门管理
	pagesMsgDatarulesTitle      = "MsgDatarulesTitle"      // 数据权限
	pagesMsgFieldRequired       = "MsgFieldRequired"       // 必填字段不能为空
	pagesMsgAdminGenericFailed  = "MsgAdminGenericFailed"  // 操作失败，请检查输入或联系管理员
)

// AdminPagesHandle 六领域管理页处理器。
// 六个契约由装配层以同一合并 Service 传入（admin 合并模块同包直调）。
type AdminPagesHandle struct {
	admins admincontract.AdminService
	roles  admincontract.RoleService
	perms  admincontract.PermService
	menus  admincontract.MenuService
	depts  admincontract.DeptService
	rules  admincontract.RuleService
}

// NewAdminPagesHandle 构造。
func NewAdminPagesHandle(admins admincontract.AdminService, roles admincontract.RoleService,
	perms admincontract.PermService, menus admincontract.MenuService,
	depts admincontract.DeptService, rules admincontract.RuleService) *AdminPagesHandle {
	return &AdminPagesHandle{admins: admins, roles: roles, perms: perms, menus: menus, depts: depts, rules: rules}
}

// --- 管理员 administrators ---

// AdministratorsPage 管理员列表页（GET /admin/administrators）。
// 分页列表 + 行内编辑/删除；新建走顶部表单。
func (h *AdminPagesHandle) AdministratorsPage(c *gin.Context) {
	res, err := h.admins.AdminList(c.Request.Context(), &admindto.AdminListReq{
		AdminPageReq: admindto.AdminPageReq{Page: 1, Limit: 100},
	})
	if err != nil {
		c.String(http.StatusInternalServerError, pagesMsgAdminGenericFailed)
		return
	}
	c.HTML(http.StatusOK, "admin/administrators", shell.Prepare(c, gin.H{
		"title": pagesMsgAdministratorsTitle,
		"menu":  "admins",
		"Rows":  res.List,
		"Total": res.Total,
	}))
}

// AdministratorsCreate 新建管理员（POST /admin/administrators/create）。
// service 强制 status=启用；必填 username/email/password(≥6)。
func (h *AdminPagesHandle) AdministratorsCreate(c *gin.Context) {
	username := shell.FieldValue(c, "username")
	email := shell.FieldValue(c, "email")
	password := shell.FieldValue(c, "password")
	if username == "" || email == "" || password == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if _, err := h.admins.AdminCreate(c.Request.Context(), &admindto.AdminCreateReq{
		Username: username, Email: email, Password: password,
		Phone: shell.FieldValue(c, "phone"), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// AdministratorsUpdate 编辑管理员（POST /admin/administrators/update）。
// AdminEdit 仅更新 username/phone/email/remark。
func (h *AdminPagesHandle) AdministratorsUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if _, err := h.admins.AdminEdit(c.Request.Context(), &admindto.AdminEditReq{
		Id: id, Username: shell.FieldValue(c, "username"), Phone: shell.FieldValue(c, "phone"),
		Email: shell.FieldValue(c, "email"), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// AdministratorsDelete 删除管理员（POST /admin/administrators/delete）。
func (h *AdminPagesHandle) AdministratorsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{Id: []uint64{id}}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// --- 角色 roles ---

// RolesPage 角色列表页（GET /admin/roles）。
func (h *AdminPagesHandle) RolesPage(c *gin.Context) {
	res, err := h.roles.RoleList(c.Request.Context(), &admindto.RoleListReq{RolePageReq: admindto.RolePageReq{Page: 1, Limit: 100}})
	if err != nil {
		c.String(http.StatusInternalServerError, pagesMsgAdminGenericFailed)
		return
	}
	c.HTML(http.StatusOK, "admin/roles", shell.Prepare(c, gin.H{
		"title": pagesMsgRolesTitle,
		"menu":  "roles",
		"Rows":  res.List,
		"Total": res.Total,
	}))
}

// RolesCreate 新建角色（POST /admin/roles/create）。
func (h *AdminPagesHandle) RolesCreate(c *gin.Context) {
	roleCode := shell.FieldValue(c, "role_code")
	roleName := shell.FieldValue(c, "role_name")
	if roleCode == "" || roleName == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.roles.RoleCreate(c.Request.Context(), &admindto.RoleCreateReq{
		RoleCode: roleCode, RoleName: roleName, Status: shell.ParseStatusPtr(c.PostForm("status")),
		SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesUpdate 编辑角色（POST /admin/roles/update）。
func (h *AdminPagesHandle) RolesUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	roleName := shell.FieldValue(c, "role_name")
	if id == 0 || roleName == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.roles.RoleUpdate(c.Request.Context(), &admindto.RoleUpdateReq{
		ID: id, RoleName: roleName, Status: shell.ParseStatus(c.PostForm("status")),
		SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesDelete 删除角色（POST /admin/roles/delete）。
func (h *AdminPagesHandle) RolesDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// --- 权限点 permissions ---

// PermissionsPage 权限资源列表页（GET /admin/permissions）。
func (h *AdminPagesHandle) PermissionsPage(c *gin.Context) {
	page, limit := shell.PageParams(c)
	// 服务端筛选：筛选条件进 SQL（与分页同源），避免「前端只过滤当前页」的错配。
	code := strings.TrimSpace(c.Query("code"))
	module := strings.TrimSpace(c.Query("module"))
	res, err := h.perms.PermList(c.Request.Context(), &admindto.PermListReq{
		PermPageReq: admindto.PermPageReq{Page: page, Limit: limit},
		Code:        code,
		Module:      module,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, pagesMsgAdminGenericFailed)
		return
	}
	data := shell.Prepare(c, gin.H{
		"title":        pagesMsgPermissionsTitle,
		"menu":         "permissions",
		"Rows":         res.List,
		"Total":        res.Total,
		"FilterCode":   code,
		"FilterModule": module,
	})
	base := shell.FilterBaseURL("/admin/permissions", map[string]string{"code": code, "module": module})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/permissions", data)
}

// PermissionsCreate 新建权限点（POST /admin/permissions/create）。
// permission_code 创建后不可修改；api_method 限 GET/POST。
func (h *AdminPagesHandle) PermissionsCreate(c *gin.Context) {
	code := shell.FieldValue(c, "permission_code")
	name := shell.FieldValue(c, "permission_name")
	module := shell.FieldValue(c, "module")
	apiPath := shell.FieldValue(c, "api_path")
	apiMethod := strings.ToUpper(shell.FieldValue(c, "api_method"))
	if code == "" || name == "" || module == "" || apiPath == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if apiMethod != "GET" && apiMethod != "POST" {
		apiMethod = "POST"
	}
	if _, err := h.perms.PermCreate(c.Request.Context(), &admindto.PermCreateReq{
		PermissionCode: code, PermissionName: name, Module: module,
		APIPath: apiPath, APIMethod: apiMethod, Status: shell.ParseStatus(c.PostForm("status")),
		Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/permissions")
}

// PermissionsUpdate 编辑权限点（POST /admin/permissions/update）。
func (h *AdminPagesHandle) PermissionsUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	name := shell.FieldValue(c, "permission_name")
	module := shell.FieldValue(c, "module")
	apiPath := shell.FieldValue(c, "api_path")
	apiMethod := strings.ToUpper(shell.FieldValue(c, "api_method"))
	if id == 0 || name == "" || module == "" || apiPath == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if apiMethod != "GET" && apiMethod != "POST" {
		apiMethod = "POST"
	}
	if _, err := h.perms.PermUpdate(c.Request.Context(), &admindto.PermUpdateReq{
		ID: id, PermissionCode: shell.FieldValue(c, "permission_code"),
		PermissionName: name, Module: module, APIPath: apiPath, APIMethod: apiMethod,
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/permissions")
}

// PermissionsDelete 删除权限点（POST /admin/permissions/delete）。
func (h *AdminPagesHandle) PermissionsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/permissions")
}

// --- 菜单 menus（树） ---

// adminMenuRow 菜单树展平行。
type adminMenuRow struct {
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

// flattenAdminMenuTree 深度优先展平菜单树为带缩进的行（Indent 控制前端层级展示）。
func flattenAdminMenuTree(nodes []admindto.MenuTreeNode, depth int, out *[]adminMenuRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		*out = append(*out, adminMenuRow{
			ID: n.ID, Title: n.Title, Path: n.Path, Type: n.Type, TypeLabel: adminMenuTypeLabel(n.Type),
			ParentID: n.ParentID, Status: n.Status, SortOrder: n.SortOrder,
			Remark: n.Remark, Indent: indent, Icon: n.Icon,
		})
		if len(n.Children) > 0 {
			flattenAdminMenuTree(n.Children, depth+1, out)
		}
	}
}

func adminMenuTypeLabel(t int) string {
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
func (h *AdminPagesHandle) MenusPage(c *gin.Context) {
	nodes, err := h.menus.MenuTree(c.Request.Context(), &admindto.MenuTreeReq{})
	if err != nil {
		c.String(http.StatusInternalServerError, pagesMsgAdminGenericFailed)
		return
	}
	rows := make([]adminMenuRow, 0, 64)
	flattenAdminMenuTree(nodes, 1, &rows)
	// 供新建下拉的父级选项（树扁平行，含全部分级）。
	c.HTML(http.StatusOK, "admin/menus", shell.Prepare(c, gin.H{
		"title":   pagesMsgMenusTitle,
		"menu":    "menus",
		"Rows":    rows,
		"Parents": rows,
	}))
}

// MenusCreate 新建菜单（POST /admin/menus/create）。
func (h *AdminPagesHandle) MenusCreate(c *gin.Context) {
	title := shell.FieldValue(c, "title")
	if title == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.menus.MenuCreate(c.Request.Context(), &admindto.MenuCreateReq{
		Title: title, ParentID: shell.ParseUint(c.PostForm("parent_id")),
		Type: shell.ParseStatus(c.PostForm("type")), Path: shell.FieldValue(c, "path"),
		Status: shell.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// MenusUpdate 编辑菜单（POST /admin/menus/update）。
func (h *AdminPagesHandle) MenusUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	title := shell.FieldValue(c, "title")
	if id == 0 || title == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.menus.MenuUpdate(c.Request.Context(), &admindto.MenuUpdateReq{
		ID: id, Title: title, ParentID: shell.ParseUint(c.PostForm("parent_id")),
		Type: shell.ParseStatus(c.PostForm("type")), Path: shell.FieldValue(c, "path"),
		Status: shell.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// MenusDelete 删除菜单（POST /admin/menus/delete）。
func (h *AdminPagesHandle) MenusDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// --- 部门 departments（树） ---

// adminDeptRow 部门树展平行。
type adminDeptRow struct {
	ID        uint64
	DeptName  string
	DeptCode  string
	ParentID  uint64
	Status    int
	SortOrder int
	Remark    string
	Indent    string
}

// flattenAdminDeptTree 深度优先展平部门树为带缩进的行（顶层为值切片，Children 为指针切片）。
func flattenAdminDeptTree(nodes []admindto.DeptTreeNode, depth int, out *[]adminDeptRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		*out = append(*out, adminDeptRow{
			ID: n.ID, DeptName: n.DeptName, DeptCode: n.DeptCode, ParentID: n.ParentID,
			Status: n.Status, SortOrder: n.SortOrder, Remark: n.Remark, Indent: indent,
		})
		if len(n.Children) > 0 {
			flattenAdminDeptNodePtrs(n.Children, depth+1, out)
		}
	}
}

// flattenAdminDeptNodePtrs 递归处理部门子树（Children 为指针切片）。
func flattenAdminDeptNodePtrs(nodes []*admindto.DeptTreeNode, depth int, out *[]adminDeptRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		if n == nil {
			continue
		}
		*out = append(*out, adminDeptRow{
			ID: n.ID, DeptName: n.DeptName, DeptCode: n.DeptCode, ParentID: n.ParentID,
			Status: n.Status, SortOrder: n.SortOrder, Remark: n.Remark, Indent: indent,
		})
		if len(n.Children) > 0 {
			flattenAdminDeptNodePtrs(n.Children, depth+1, out)
		}
	}
}

// DepartmentsPage 部门管理页（GET /admin/departments）。
func (h *AdminPagesHandle) DepartmentsPage(c *gin.Context) {
	nodes, err := h.depts.DeptTree(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, pagesMsgAdminGenericFailed)
		return
	}
	rows := make([]adminDeptRow, 0, 64)
	flattenAdminDeptTree(nodes, 1, &rows)
	c.HTML(http.StatusOK, "admin/departments", shell.Prepare(c, gin.H{
		"title":   pagesMsgDepartmentsTitle,
		"menu":    "depts",
		"Rows":    rows,
		"Parents": rows,
	}))
}

// DepartmentsCreate 新建部门（POST /admin/departments/create）。
func (h *AdminPagesHandle) DepartmentsCreate(c *gin.Context) {
	deptName := shell.FieldValue(c, "dept_name")
	deptCode := shell.FieldValue(c, "dept_code")
	if deptName == "" || deptCode == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.depts.DeptCreate(c.Request.Context(), &admindto.DeptCreateReq{
		ParentID: shell.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// DepartmentsUpdate 编辑部门（POST /admin/departments/update）。
func (h *AdminPagesHandle) DepartmentsUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	deptName := shell.FieldValue(c, "dept_name")
	deptCode := shell.FieldValue(c, "dept_code")
	if id == 0 || deptName == "" || deptCode == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.depts.DeptUpdate(c.Request.Context(), &admindto.DeptUpdateReq{
		ID: id, ParentID: shell.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// DepartmentsDelete 删除部门（POST /admin/departments/delete）。
func (h *AdminPagesHandle) DepartmentsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// --- 数据权限 datarules ---

// DatarulesPage 数据权限列表页（GET /admin/datarules）。
// 列表 + 新建（domain 下拉来自已注册数据域）+ 删除；编辑走独立 /edit?id= 页（detail 回显，配置复杂）。
func (h *AdminPagesHandle) DatarulesPage(c *gin.Context) {
	res, err := h.rules.RuleList(c.Request.Context(), &admindto.RuleListReq{Page: 1, Limit: 100})
	if err != nil {
		c.String(http.StatusInternalServerError, pagesMsgAdminGenericFailed)
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	c.HTML(http.StatusOK, "admin/datarules", shell.Prepare(c, gin.H{
		"title":   pagesMsgDatarulesTitle,
		"menu":    "datarules",
		"Rows":    res.List,
		"Total":   res.Total,
		"Domains": domains,
	}))
}

// DatarulesCreate 新建数据规则（POST /admin/datarules/create）。
// config 为可选 JSON 文本（RuleConfig；缺省给空配置 {}）。
func (h *AdminPagesHandle) DatarulesCreate(c *gin.Context) {
	ruleName := shell.FieldValue(c, "rule_name")
	domain := shell.FieldValue(c, "domain")
	if ruleName == "" || domain == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	config, err := adminConfigFromJSON(shell.FieldValue(c, "config"))
	if err != nil {
		c.String(http.StatusBadRequest, "配置 JSON 不合法")
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: config,
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesEditPage 数据规则编辑页（GET /admin/datarules/edit?id=X）。
// 从 RuleDetail 回显 rule_name/domain/status/remark 与 config JSON。
func (h *AdminPagesHandle) DatarulesEditPage(c *gin.Context) {
	id := shell.ParseUint(c.Query("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
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
	configJSON, err := adminConfigToJSON(detail.Config)
	if err != nil {
		configJSON = ""
	}
	c.HTML(http.StatusOK, "admin/datarule_edit", shell.Prepare(c, gin.H{
		"title":   pagesMsgDatarulesTitle,
		"menu":    "datarules",
		"Detail":  detail,
		"Domains": domains,
		"Config":  configJSON,
	}))
}

// DatarulesUpdate 保存数据规则（POST /admin/datarules/update）。
// config 为可选字段：列表抽屉表单不含该字段（列表 dto 无 config），此时沿用库中原配置，
// 避免「只改基础字段」把规则配置清空；显式提交 config（含空串）仍按提交值处理。
func (h *AdminPagesHandle) DatarulesUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	ruleName := shell.FieldValue(c, "rule_name")
	domain := shell.FieldValue(c, "domain")
	if id == 0 || ruleName == "" || domain == "" {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	config, err := adminConfigFromJSON(shell.FieldValue(c, "config"))
	if err != nil {
		c.String(http.StatusBadRequest, "配置 JSON 不合法")
		return
	}
	if _, ok := c.GetPostForm("config"); !ok {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil || detail == nil {
			shell.AdminWriteFailed(c, detailErr)
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesDelete 删除数据规则（POST /admin/datarules/delete）。
func (h *AdminPagesHandle) DatarulesDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, pagesMsgFieldRequired)
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		shell.AdminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// adminConfigToJSON 序列化 RuleConfig 为缩进 JSON 文本（编辑回显）。
func adminConfigToJSON(cfg datarule.RuleConfig) (string, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// adminConfigFromJSON 解析表单配置 JSON 文本为 RuleConfig；空文本返回空配置。
func adminConfigFromJSON(s string) (datarule.RuleConfig, error) {
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

// --- 文案词条页（审计 I18N-003） ---
//
// 迁移 seed 是默认值来源（ON CONFLICT DO NOTHING，不覆盖这里的修改）；
// 本页是真相来源（运营改过之后，seed 不会再回来覆盖）。
// 保存/删除后立即重载缓存（pkg/i18n 内部完成）。

// adminI18nEntryPageSize 每页条数。
const adminI18nEntryPageSize = 50

// adminI18nEntryHandle 词条页处理器（无依赖：读写都走 pkg/i18n 的端口）。
type adminI18nEntryHandle struct{}

// NewAdminI18nEntryHandle 构造。
func NewAdminI18nEntryHandle() *adminI18nEntryHandle { return &adminI18nEntryHandle{} }

// adminI18nEntryFilterOf 读取筛选参数（GET，全部可选）。
func adminI18nEntryFilterOf(c *gin.Context) i18n.EntryFilter {
	page, _ := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	if page < 1 {
		page = 1
	}
	return i18n.EntryFilter{
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Lang:     strings.TrimSpace(c.Query("lang")),
		Category: strings.TrimSpace(c.Query("category")),
		Limit:    adminI18nEntryPageSize,
		Offset:   (page - 1) * adminI18nEntryPageSize,
	}
}

// I18nEntriesPage GET /admin/i18n —— 词条列表与编辑入口。
func (h *adminI18nEntryHandle) I18nEntriesPage(c *gin.Context) {
	filter := adminI18nEntryFilterOf(c)
	items, total, err := i18n.ListEntries(c.Request.Context(), filter)
	if err != nil {
		shell.PageError(c, "i18n_entry", err)
		return
	}
	categories, _ := i18n.Categories(c.Request.Context())
	page := filter.Offset/adminI18nEntryPageSize + 1
	if page < 1 {
		page = 1
	}
	pages := int((total + adminI18nEntryPageSize - 1) / adminI18nEntryPageSize)
	data := gin.H{
		"title":      "文案词条",
		"Entries":    items,
		"Total":      total,
		"Page":       page,
		"Pages":      pages,
		"Keyword":    filter.Keyword,
		"LangFilter": filter.Lang,
		"CatFilter":  filter.Category,
		"Categories": categories,
		"Saved":      strings.TrimSpace(c.Query("saved")),
		"Errored":    strings.TrimSpace(c.Query("errored")),
	}
	c.HTML(http.StatusOK, "admin/i18n", shell.Prepare(c, data))
}

// I18nEntrySave POST /admin/i18n/save —— 新增或更新一条词条。
func (h *adminI18nEntryHandle) I18nEntrySave(c *gin.Context) {
	entry := i18n.Entry{
		Key:      c.PostForm("key"),
		Lang:     c.PostForm("lang"),
		Value:    c.PostForm("value"),
		Category: c.PostForm("category"),
		Remark:   c.PostForm("remark"),
		Status:   1,
	}
	if err := i18n.SaveEntry(c.Request.Context(), entry); err != nil {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", err.Error()))
		return
	}
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "saved", strings.TrimSpace(entry.Key)+" · "+strings.TrimSpace(entry.Lang)))
}

// I18nEntryDelete POST /admin/i18n/delete —— 删除一条词条。
// 删除后构建期回退到组件包内的中文兜底（可见降级，不是空白）。
func (h *adminI18nEntryHandle) I18nEntryDelete(c *gin.Context) {
	key := c.PostForm("key")
	lang := c.PostForm("lang")
	if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", err.Error()))
		return
	}
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "saved", "已删除 "+strings.TrimSpace(key)+" · "+strings.TrimSpace(lang)))
}

// adminI18nBackURL 回列表并带上筛选与提示（只回填站内相对路径，避免开放重定向）。
func adminI18nBackURL(c *gin.Context, hintKey, hintValue string) string {
	q := []string{}
	for _, k := range []string{"keyword", "lang", "category", "page"} {
		value := strings.TrimSpace(c.PostForm("_" + k))
		if value == "" {
			value = strings.TrimSpace(c.Query(k))
		}
		if value != "" {
			q = append(q, k+"="+url.QueryEscape(value))
		}
	}
	q = append(q, hintKey+"="+url.QueryEscape(hintValue))
	return "/admin/i18n?" + strings.Join(q, "&")
}

// --- 语言切换 ---

// AdminLangSwitch 处理 GET /admin/lang：写语言 Cookie 后 302 回跳。
//
// query 参数：
//   - lang：目标语言（zh / zh-CN / en / en-US 等变体，经 response.NormalizeLang 规范化）；
//     缺失或非法时忽略该值并回退配置默认语言（不报错）。
//   - redirect：回跳地址；仅接受站内路径，否则回首页，防开放重定向。
//
// GET 属安全方法，CSRF 直接放行；Cookie 为 SameSite=Lax + Path=/。
func AdminLangSwitch(c *gin.Context) {
	lang, _ := response.NormalizeLang(c.Query("lang"))
	response.SetLangCookie(c, lang)

	c.Redirect(http.StatusFound, adminSafeLangRedirect(c.Query("redirect")))
}

// adminSafeLangRedirect 校验回跳地址：只允许站内绝对路径，其余一律回首页 "/"。
// 拒绝：不以 "/" 开头、含 "//"、含反斜杠或控制字符。
func adminSafeLangRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return "/"
	}
	if strings.Contains(raw, "//") {
		return "/"
	}
	if strings.ContainsAny(raw, "\\\r\n\t") {
		return "/"
	}
	return raw
}

// AdminLoginPage 后台登录页（独立布局，供未登录的页面请求 302 跳转，也支持直接访问）。
// 验证码经 /api/captcha 返回图片（答案不下发），此处渲染页面骨架即可；
// 若渲染入口已生成验证码图片则直接注入，避免首屏额外请求。
func AdminLoginPage(c *gin.Context) {
	id, image := captcha.Get().GenerateImage()
	// 登录页无会话（不走 Prepare 的权限集部分），同样需要语言数据：
	// 标题走 shell.login.title（缺词条回退「登录」）。
	c.HTML(http.StatusOK, "admin/login", shell.Prepare(c, gin.H{
		"title":         shell.TranslateFor(c)("shell.login.title", "登录"),
		"captcha_id":    id,
		"captcha_image": image,
		// debug 模式才显示一键登录入口（release 下路由压根不存在，显示了也是个死链）。
		"DevLogin": AdminDevLoginEnabled(),
	}))
}

// AdminDevLoginEnabled 是否处于 debug 模式（决定登录页是否显示一键登录入口）。
// 与 routers 里注册 /admin/dev-login 用的是同一个判断：两处必须一致。
func AdminDevLoginEnabled() bool {
	v, err := config.GetViper()
	return err == nil && strings.EqualFold(v.GetString("server.mode"), "debug")
}
