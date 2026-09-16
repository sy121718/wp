package dashboardhttp

import (
	"net/http"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"

	"github.com/gin-gonic/gin"
)

// admin_org_handle.go - 后台组织管理页：权限菜单树与部门树（列表 / 增删改）。

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
