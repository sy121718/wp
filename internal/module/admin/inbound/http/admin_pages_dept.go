package adminhttp

// admin_pages_dept.go — 部门域管理页：部门树展平、列表、CRUD 与批量删除（/admin/departments）。

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"

	admindto "go_wp/internal/module/admin/dto"
)

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
	page, limit := shell.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	res, err := h.depts.DeptPage(c.Request.Context(), page, limit, keyword)
	if err != nil {
		res = &admindto.DeptPageResp{}
	}
	data := shell.Prepare(c, gin.H{
		"title": pagesMsgDepartmentsTitle, "menu": "depts",
		"Rows": res.Rows, "Parents": res.Parents, "Total": res.Total,
		"FilterKeyword": keyword,
		"Err":           adminErrOrLoad(c, err), "Done": adminPageDone(c, c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/departments", map[string]string{"keyword": keyword})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/departments", data)
}

// DepartmentsCreate 新建部门（POST /admin/departments/create）。
func (h *AdminPagesHandle) DepartmentsCreate(c *gin.Context) {
	deptName := shell.FieldValue(c, "dept_name")
	deptCode := shell.FieldValue(c, "dept_code")
	if deptName == "" || deptCode == "" {
		adminPageParamFail(c, "/admin/departments")
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.depts.DeptCreate(c.Request.Context(), &admindto.DeptCreateReq{
		ParentID: shell.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/departments", err)
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
		adminPageParamFail(c, "/admin/departments")
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.depts.DeptUpdate(c.Request.Context(), &admindto.DeptUpdateReq{
		ID: id, ParentID: shell.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/departments", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// DepartmentsDelete 删除部门（POST /admin/departments/delete）。
func (h *AdminPagesHandle) DepartmentsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, "/admin/departments")
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
		adminPageWriteFail(c, "/admin/departments", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/departments")
}

// DepartmentsBulkDelete 批量删除部门（POST /admin/departments/bulk-delete）。
//
// 有子部门或部门下有管理员的由 service 拒绝、其余照常删除；单条失败只计数不中断整批。
func (h *AdminPagesHandle) DepartmentsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusSeeOther, "/admin/departments?err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	c.Redirect(http.StatusSeeOther, adminBulkResultURL(c, "/admin/departments", adminBulkNounDept, deleted, skipped))
}
