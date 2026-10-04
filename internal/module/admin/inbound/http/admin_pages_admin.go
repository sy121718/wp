package adminhttp

// admin_pages_admin.go — 管理员域管理页：列表、新建、编辑、单条删除、批量删除（/admin/administrators）。

import (
	"net/http"
	"net/url"
	"strings"

	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"

	admindto "go_wp/internal/module/admin/dto"
)

// --- 管理员 administrators ---

// AdministratorsPage 管理员列表页（GET /admin/administrators）。
// 分页列表 + 行内编辑/删除；新建走顶部表单。
//
// 服务端筛选（与分页同源，翻页保留筛选）：name / email 交给 service 的 SQL 条件，
// handler 不做内存过滤 —— 内存过滤只能筛当前页，会让「共 N」与实际结果互相矛盾。
// 回显键名对齐模板：administrators.html 的 value="{{.["FilterName"]}}" / "{{.["FilterEmail"]}}"。
func (h *AdminPagesHandle) AdministratorsPage(c *gin.Context) {
	page, limit := shell.PageParams(c)
	name := strings.TrimSpace(c.Query("name"))
	email := strings.TrimSpace(c.Query("email"))
	res, err := h.admins.AdminList(c.Request.Context(), &admindto.AdminListReq{
		AdminPageReq: admindto.AdminPageReq{Page: page, Limit: limit},
		Name:         name,
		Email:        email,
	})
	if err != nil {
		// 降级渲染：res 换零值、页面照常渲染（详见 adminErrOrLoad 的说明）。
		res = &admindto.AdminListResp{}
	}
	data := shell.Prepare(c, gin.H{
		"title":       pagesMsgAdministratorsTitle,
		"menu":        "admins",
		"Rows":        res.List,
		"Total":       res.Total,
		"FilterName":  name,
		"FilterEmail": email,
		"Err":         adminErrOrLoad(c, err),
		"Done":        adminPageDone(c, c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/administrators", map[string]string{"name": name, "email": email})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/administrators", data)
}

// AdministratorsCreate 新建管理员（POST /admin/administrators/create）。
// service 强制 status=启用；必填 username/email/password(≥6)。
func (h *AdminPagesHandle) AdministratorsCreate(c *gin.Context) {
	username := shell.FieldValue(c, "username")
	email := shell.FieldValue(c, "email")
	password := shell.FieldValue(c, "password")
	if username == "" || email == "" || password == "" {
		adminPageParamFail(c, "/admin/administrators")
		return
	}
	if _, err := h.admins.AdminCreate(c.Request.Context(), &admindto.AdminCreateReq{
		Username: username, Email: email, Password: password,
		Phone: shell.FieldValue(c, "phone"), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/administrators", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// AdministratorsUpdate 编辑管理员（POST /admin/administrators/update）。
// AdminEdit 仅更新 username/phone/email/remark。
func (h *AdminPagesHandle) AdministratorsUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, "/admin/administrators")
		return
	}
	if _, err := h.admins.AdminEdit(c.Request.Context(), &admindto.AdminEditReq{
		Id: id, Username: shell.FieldValue(c, "username"), Phone: shell.FieldValue(c, "phone"),
		Email: shell.FieldValue(c, "email"), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/administrators", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// AdministratorsDelete 删除管理员（POST /admin/administrators/delete）。
//
// OperatorID 必须从会话注入：AdminDelete 的「不能删自己」判定依赖它，缺省 0 时该判定
// 静默失效（只剩超管保护兜底）—— 单条端点此前漏了注入，而批量端点从一开始就带，
// 于是出现「批量比单条更严」的错位。
func (h *AdminPagesHandle) AdministratorsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, "/admin/administrators")
		return
	}
	if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{
		Id: []uint64{id}, OperatorID: shell.CurrentUserID(c),
	}); err != nil {
		adminPageWriteFail(c, "/admin/administrators", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// AdministratorsBulkDelete 批量删除管理员（POST /admin/administrators/bulk-delete）。
//
// 逐条走同一条单条删除路径：删自己、超管由 service 拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 操作者 id 从会话注入（页面的单条删除没有注入，这里补上，否则「不能删自己」判定不生效）。
func (h *AdminPagesHandle) AdministratorsBulkDelete(c *gin.Context) {
	operatorID := shell.CurrentUserID(c)
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusSeeOther, "/admin/administrators?err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			continue
		}
		if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{
			Id: []uint64{id}, OperatorID: operatorID,
		}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	c.Redirect(http.StatusSeeOther, adminBulkResultURL(c, "/admin/administrators", adminBulkNounAdmin, deleted, skipped))
}
