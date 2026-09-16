package dashboardhttp

import (
	"net/http"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"

	"github.com/gin-gonic/gin"
)

// admin_permission_handle.go - 后台权限点管理页（列表 / 增删改）。

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
