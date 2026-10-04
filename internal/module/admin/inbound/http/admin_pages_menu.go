package adminhttp

// admin_pages_menu.go — 菜单域管理页：菜单树展平、菜单页、菜单绑定权限点的候选项装配、CRUD 与批量删除（/admin/menus）。

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/web/shell"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
)

// adminMenuTreeHas 菜单树里是否存在该 id（单条删除的存在性预检，见 MenusDelete）。
func adminMenuTreeHas(nodes []admindto.MenuTreeNode, id uint64) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
		if adminMenuTreeHas(n.Children, id) {
			return true
		}
	}
	return false
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
//
// 注意：菜单管理页（menus.html）自己按 m.Type 取词渲染徽章，本函数目前**没有调用方**。
// 保留并按同一形态取词，是为了它将来接上模板时不会退回中文硬编码。
func flattenAdminMenuTree(tr adminLabelTranslate, nodes []admindto.MenuTreeNode, depth int, out *[]adminMenuRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		*out = append(*out, adminMenuRow{
			ID: n.ID, Title: n.Title, Path: n.Path, Type: n.Type, TypeLabel: adminMenuTypeLabel(tr, n.Type),
			ParentID: n.ParentID, Status: n.Status, SortOrder: n.SortOrder,
			Remark: n.Remark, Indent: indent, Icon: n.Icon,
		})
		if len(n.Children) > 0 {
			flattenAdminMenuTree(tr, n.Children, depth+1, out)
		}
	}
}

// MenusPage 菜单管理页（GET /admin/menus）。
func (h *AdminPagesHandle) MenusPage(c *gin.Context) {
	page, limit := shell.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	res, err := h.menus.MenuPage(c.Request.Context(), page, limit, keyword)
	if err != nil {
		res = &admindto.MenuPageResp{}
	}
	data := shell.Prepare(c, gin.H{
		"title": pagesMsgMenusTitle, "menu": "menus",
		"Rows": res.Rows, "Parents": res.Parents, "Total": res.Total,
		"FilterKeyword": keyword,
		"Err":           adminErrOrLoad(c, err), "Done": adminPageDone(c, c.Query("done")),
		// 新建抽屉内嵌在本页，它的权限点候选也在这里给（新建时没有任何已绑码）。
		"PermChoices": h.buildPermChoices(c, nil),
	})
	base := shell.FilterBaseURL("/admin/menus", map[string]string{"keyword": keyword})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/menus", data)
}

// MenusEditFragment returns one existing menu's edit form.
func (h *AdminPagesHandle) MenusEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := shell.ParseUint(c.Query("id"))
	if id == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: id})
	if err != nil {
		adminErrParam(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	if row == nil || row.ID != id {
		c.Status(http.StatusNotFound)
		return
	}
	c.HTML(http.StatusOK, "admin/system/menu_edit_form.html", shell.Prepare(c, gin.H{
		"MenuEdit": row, "PermChoices": h.buildPermChoices(c, row.PermissionCodes),
	}))
}

// --- 菜单绑定权限点（一个菜单可以挂多个码，迁移 470）---

// permChoiceItem 是菜单表单里的一行权限点候选项。
//
// Checked 在 Go 侧算好再交给模板：Jet 里对 map 做链式索引（.CheckedMap[code]）既容易踩
// 类型与缺键的坑，也不符合本项目的模板约定（模板只渲染、不做判断）——
// 与 permTreeRow.Checked 同一取舍。
type permChoiceItem struct {
	Code    string
	Name    string
	Checked bool
}

// permChoiceGroup 按 module 分组的候选项。
//
// 为什么要分组：候选来自 sys_permission，全量约 300 条、31 个模块，平铺成一长条清单
// 没法用（找不到、也没法判断「这个码属于哪块业务」）。分组标题就是 module 本身。
type permChoiceGroup struct {
	Module string
	Items  []permChoiceItem
}

// buildPermChoices 把启用权限点按 module 分组，并标出目标菜单已经绑了哪些码。
//
// selected 是当前菜单的码集合（新建时为空）。**不做任何「只能绑一个」的限制**：
// 一个菜单挂几个码正是这一版的核心能力。空码由 model 侧去重时丢掉，这里不预筛。
func (h *AdminPagesHandle) buildPermChoices(c *gin.Context, selected []string) []permChoiceGroup {
	res, err := h.perms.PermOptions(c.Request.Context(), &admindto.PermOptionsReq{})
	if err != nil || res == nil {
		// 取不到候选清单不能拖垮整个表单：页面照常渲染，只是这一屏没有可勾的权限点。
		// 原文只进结构化日志，响应里不出现内部细节。
		if err != nil {
			adminErrParam(c, err)
		}
		return nil
	}
	picked := make(map[string]struct{}, len(selected))
	for _, code := range selected {
		picked[code] = struct{}{}
	}
	// PermOptions 已按 module, id 排序，顺序扫描即可分组（组内保持 id 序）。
	groups := make([]permChoiceGroup, 0, 8)
	index := make(map[string]int, 8)
	for _, opt := range res.List {
		i, ok := index[opt.Module]
		if !ok {
			i = len(groups)
			index[opt.Module] = i
			groups = append(groups, permChoiceGroup{Module: opt.Module})
		}
		_, on := picked[opt.PermissionCode]
		groups[i].Items = append(groups[i].Items, permChoiceItem{
			Code: opt.PermissionCode, Name: opt.PermissionName, Checked: on,
		})
	}
	return groups
}

func (h *AdminPagesHandle) menuEditFail(c *gin.Context, msg string) {
	if !adminDrawerHX(c) {
		c.Redirect(http.StatusSeeOther, adminPageErrURL("/admin/menus", msg))
		return
	}
	id := shell.ParseUint(c.PostForm("id"))
	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: id})
	if id == 0 || err != nil || row == nil || row.ID != id {
		adminDrawerRedirect(c, adminPageErrURL("/admin/menus", msg))
		return
	}
	data := gin.H{"MenuEdit": row, "MenuEditErr": msg, "MenuEditEcho": map[string]string{
		"title": c.PostForm("title"), "parent_id": c.PostForm("parent_id"),
		"type": c.PostForm("type"), "status": c.PostForm("status"),
		"path": c.PostForm("path"), "icon": c.PostForm("icon"),
		"sort_order": c.PostForm("sort_order"), "remark": c.PostForm("remark"),
	},
		// 权限码是多值，塞不进上面那个 map[string]string；模板优先用它回显用户刚勾的那一屏，
		// 否则保存失败后勾选会退回库里的旧值，而用户正需要在这里改掉那个错误重试。
		// 候选项也要按**本次提交**重算勾选态，否则错误响应里的清单和回显的码对不上。
		"MenuEditEchoCodes": menuPermissionCodes(c),
		"PermChoices":       h.buildPermChoices(c, menuPermissionCodes(c))}
	c.HTML(http.StatusOK, "admin/system/menu_edit_form.html", shell.Prepare(c, data))
}

// menuPermissionCodes 读取表单里的权限码集合（多选控件提交 permission_codes 的多个值）。
//
// 空项在这里丢掉：控件「一个都没选」时会提交一个空字符串，那不是「绑了一个空权限码」——
// 放任它进 service 会命中 ErrCodeNotEnabled（一个不存在的码），把「没选」报成「码无效」。
func menuPermissionCodes(c *gin.Context) []string {
	raw := c.PostFormArray("permission_codes")
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// MenusCreate 新建菜单（POST /admin/menus/create）。
func (h *AdminPagesHandle) MenusCreate(c *gin.Context) {
	title := shell.FieldValue(c, "title")
	if title == "" {
		adminPageParamFail(c, "/admin/menus")
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.menus.MenuCreate(c.Request.Context(), &admindto.MenuCreateReq{
		Title: title, ParentID: shell.ParseUint(c.PostForm("parent_id")),
		Type: shell.ParseStatus(c.PostForm("type")), Path: shell.FieldValue(c, "path"),
		Status: shell.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
		PermissionCodes: menuPermissionCodes(c),
	}); err != nil {
		adminPageWriteFail(c, "/admin/menus", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// MenusUpdate 编辑菜单（POST /admin/menus/update）。
func (h *AdminPagesHandle) MenusUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	title := shell.FieldValue(c, "title")
	if id == 0 || title == "" {
		if adminDrawerHX(c) {
			h.menuEditFail(c, response.TranslateMessage(c, adminenums.MsgBadRequest))
			return
		}
		adminPageParamFail(c, "/admin/menus")
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.menus.MenuUpdate(c.Request.Context(), &admindto.MenuUpdateReq{
		ID: id, Title: title, ParentID: shell.ParseUint(c.PostForm("parent_id")),
		Type: shell.ParseStatus(c.PostForm("type")), Path: shell.FieldValue(c, "path"),
		Status: shell.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
		PermissionCodes: menuPermissionCodes(c),
	}); err != nil {
		if adminDrawerHX(c) {
			h.menuEditFail(c, adminErrParam(c, err))
			return
		}
		adminPageWriteFail(c, "/admin/menus", err)
		return
	}
	adminDrawerRedirect(c, "/admin/menus")
}

// MenusDelete 删除菜单（POST /admin/menus/delete）。
//
// 单条删除先在树里确认目标存在：MenuDelete 是**按 id 列表**删除（逐条 GetByID，查不到就
// continue），所以「删一个已经被别人删掉的菜单」既不报错也不删任何东西 —— 表现为 303 回列表、
// 页面上什么都没发生，用户只能反复点。用同一棵树（页面本来就要读它）做一次存在性预检，
// 把这一支变成一条可见的提示。检查系统菜单 / 子菜单仍由 service 判定，这里只判「在不在」。
func (h *AdminPagesHandle) MenusDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, "/admin/menus")
		return
	}
	nodes, err := h.menus.MenuTree(c.Request.Context(), &admindto.MenuTreeReq{})
	if err != nil {
		adminPageWriteFail(c, "/admin/menus", err)
		return
	}
	if !adminMenuTreeHas(nodes, id) {
		adminPageWriteFail(c, "/admin/menus", errors.New(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
		adminPageWriteFail(c, "/admin/menus", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/menus")
}

// MenusBulkDelete 批量删除菜单（POST /admin/menus/bulk-delete）。
//
// 系统菜单与有子菜单的节点由 service 拒绝、其余照常删除；单条失败只计数不中断整批。
func (h *AdminPagesHandle) MenusBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusSeeOther, "/admin/menus?err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	c.Redirect(http.StatusSeeOther, adminBulkResultURL(c, "/admin/menus", adminBulkNounMenu, deleted, skipped))
}
