package adminhttp

// admin_pages_perm.go — 权限点域管理页：列表、行内编辑片段、新建、编辑、单条与批量删除（/admin/permissions）。

import (
	"net/http"
	"net/url"
	"strings"

	"go_wp/internal/web/shell"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
)

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
		res = &admindto.PermListResp{}
	}
	data := shell.Prepare(c, gin.H{
		"title":        pagesMsgPermissionsTitle,
		"menu":         "permissions",
		"Rows":         res.List,
		"Total":        res.Total,
		"FilterCode":   code,
		"FilterModule": module,
		"Err":          adminErrOrLoad(c, err),
		"Done":         adminPageDone(c, c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/permissions", map[string]string{"code": code, "module": module})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/permissions", data)
}

// PermissionsEditFragment fetches one existing permission by its exact id.
func (h *AdminPagesHandle) PermissionsEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := shell.ParseUint(c.Query("id"))
	if id == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	row, err := h.perms.PermDetail(c.Request.Context(), &admindto.PermDetailReq{ID: id})
	if err != nil {
		adminErrParam(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	if row == nil || row.ID != id {
		c.Status(http.StatusNotFound)
		return
	}
	c.HTML(http.StatusOK, "admin/system/permission_edit_form.html", shell.Prepare(c, gin.H{"PermissionEdit": row}))
}

func (h *AdminPagesHandle) permissionEditFail(c *gin.Context, msg string) {
	if !adminDrawerHX(c) {
		c.Redirect(http.StatusSeeOther, adminPageErrURL(adminPermissionsBackURL(c), msg))
		return
	}
	id := shell.ParseUint(c.PostForm("id"))
	row, err := h.perms.PermDetail(c.Request.Context(), &admindto.PermDetailReq{ID: id})
	if id == 0 || err != nil || row == nil || row.ID != id {
		adminDrawerRedirect(c, adminPageErrURL(adminPermissionsBackURL(c), msg))
		return
	}
	data := gin.H{"PermissionEdit": row, "PermissionEditErr": msg, "PermissionEditEcho": map[string]string{
		"permission_name": c.PostForm("permission_name"), "module": c.PostForm("module"),
		"api_path": c.PostForm("api_path"), "api_method": c.PostForm("api_method"),
		"status": c.PostForm("status"), "remark": c.PostForm("remark"),
	}}
	c.HTML(http.StatusOK, "admin/system/permission_edit_form.html", shell.Prepare(c, data))
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
		adminPageParamFail(c, adminPermissionsBackURL(c))
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
		adminPageWriteFail(c, adminPermissionsBackURL(c), err)
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
		if adminDrawerHX(c) {
			h.permissionEditFail(c, response.TranslateMessage(c, adminenums.MsgBadRequest))
			return
		}
		adminPageParamFail(c, adminPermissionsBackURL(c))
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
		if adminDrawerHX(c) {
			h.permissionEditFail(c, adminErrParam(c, err))
			return
		}
		adminPageWriteFail(c, adminPermissionsBackURL(c), err)
		return
	}
	adminDrawerRedirect(c, "/admin/permissions")
}

// PermissionsDelete 删除权限点（POST /admin/permissions/delete）。
func (h *AdminPagesHandle) PermissionsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, adminPermissionsBackURL(c))
		return
	}
	if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
		adminPageWriteFail(c, adminPermissionsBackURL(c), err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/permissions")
}

// PermissionsBulkDelete 批量删除权限点（POST /admin/permissions/bulk-delete）。
//
// 已分配给角色或被菜单引用的权限点由 service 拒绝、其余照常删除；
// 单条失败只计数不中断整批。
func (h *AdminPagesHandle) PermissionsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusSeeOther, "/admin/permissions?err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			continue
		}
		if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	c.Redirect(http.StatusSeeOther, adminBulkResultURL(c, "/admin/permissions", adminBulkNounPermission, deleted, skipped))
}
