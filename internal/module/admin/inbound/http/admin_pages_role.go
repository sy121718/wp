package adminhttp

// admin_pages_role.go — 角色域管理页：角色 CRUD 与批量删除，「角色权限分配」抽屉（权限树展平 / 片段 / 保存）。

import (
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

// --- 角色 roles ---

// RolesPage 角色列表页（GET /admin/roles）。
//
// 服务端筛选：keyword 进 SQL（role_code / role_name 的 LIKE，见 model.ListAll），
// 与分页同源；回显键名对齐模板 roles.html 的 value="{{.["FilterKeyword"]}}"。
func (h *AdminPagesHandle) RolesPage(c *gin.Context) {
	page, limit := shell.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	res, err := h.roles.RoleList(c.Request.Context(), &admindto.RoleListReq{
		RolePageReq: admindto.RolePageReq{Page: page, Limit: limit},
		Keyword:     keyword,
	})
	if err != nil {
		res = &admindto.RoleListResp{}
	}
	data := shell.Prepare(c, gin.H{
		"title":         pagesMsgRolesTitle,
		"menu":          "roles",
		"Rows":          res.List,
		"Total":         res.Total,
		"FilterKeyword": keyword,
		"Err":           adminErrOrLoad(c, err),
		"Done":          adminPageDone(c, c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/roles", map[string]string{"keyword": keyword})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/roles", data)
}

// --- 角色权限分配（角色分权）---
//
// 落点说明：菜单管理（/admin/menus）管菜单**本身**的增删改，角色管理（/admin/roles）管角色的
// 元信息，两者都不负责「这个角色能用哪些菜单与按钮」。此前 /api/role/menu/list 与
// /api/role/menu/save 只有服务端实现（含权限点 seed 与超管保护），**没有任何界面调用方** ——
// 角色分权这件事实际上只能靠直接改库完成。这个抽屉就是它的落点。
//
// 形态：角色列表行的「权限分配」按钮按需拉片段（data-drawer-url → RolePermissionsDrawer），
// 保存回同页（RolePermissionsSave）。它曾经是一个独立页面，改成抽屉的理由与两条硬约束
// 写在 internal/templates/admin/system/role_permissions.html 的文件头 —— 其中一条是
// 片段里不能出现 <script>（drawer.js 的 fragmentRoot 校验），所以父子联动住在
// internal/templates/static/js/ui/perm-tree.js。

// permTreeRow 权限分配树的展平行。
//
// 用「深度优先展平 + 层级数字」而不是 Jet 递归渲染：递归要求写成 block + 独立文件 + import，
// 而每个节点还要挂勾选框、父 id、类型徽标与权限码 —— 平铺的行更好写，JS 联动只需读
// data-parent（不依赖 DOM 嵌套），折叠与搜索过滤也退化成「按 depth 连续区间隐藏」。
type permTreeRow struct {
	ID        uint64
	Title     string
	Type      int
	TypeLabel string
	Depth     int
	// PadLeft 缩进像素值，在 Go 侧算好再交给模板。
	// Jet 的表达式求值在算术上不报错但语义容易踩（整数除法、优先级），
	// 而这种「一行一个数」的计算放 Go 侧是零成本的，也让模板保持纯渲染。
	PadLeft  int
	ParentID uint64
	// PermissionCodes 是这个节点挂的全部权限码（迁移 470 起多对多，可能为空）。
	// 抽屉里逐个渲染成 <code> 徽标：管理员要能一眼看出「这个菜单代表哪些 API 权限」，
	// 因为勾选树是按菜单节点勾的，码本身不显示就会让「勾了什么」变得不可核对。
	PermissionCodes []string
	Status          int
	Checked         bool
	HasChildren     bool
}

// permRowIndentStep 每层缩进像素。
const permRowIndentStep = 20

// flattenPermissionTree 深度优先展平授权树，勾选态由已补齐祖先的 checked 集合决定。
//
// 展平保持 DFS 顺序：页面的折叠与搜索都靠「同一父级的子树在数组里连续」这一性质
// 实现（隐藏 depth 更大的连续区间），一旦顺序被打乱，折叠就会藏错行。
//
// tr 从调用点传进来（取词只在 handler 层做）：类型标签是模板直接渲染的文本，
// 不经过 pkg/response 的 translate。
func flattenPermissionTree(tr adminLabelTranslate, nodes []admindto.MenuTreeNode, depth int, checked map[uint64]struct{}, out *[]permTreeRow) {
	for _, n := range nodes {
		_, ok := checked[n.ID]
		*out = append(*out, permTreeRow{
			ID: n.ID, Title: n.Title, Type: n.Type, TypeLabel: adminMenuTypeLabel(tr, n.Type),
			Depth: depth, PadLeft: (depth-1)*permRowIndentStep + 10,
			ParentID: n.ParentID, PermissionCodes: n.PermissionCodes,
			Status: n.Status, Checked: ok, HasChildren: len(n.Children) > 0,
		})
		if len(n.Children) > 0 {
			flattenPermissionTree(tr, n.Children, depth+1, checked, out)
		}
	}
}

// rolePermissionsDrawerData 抽屉的数据装配：授权树 + 勾选集合 → 深度优先展平行。
//
// checkedOverride 非 nil 时用它代替库里的勾选集合。这是保存失败路径的关键：树要重取
// （菜单结构可能刚被别处改过），但**勾选必须用本次提交的值** —— 用库里的值会把用户
// 刚勾的那一屏整块回退成保存前的旧状态，而他正需要在这里改掉那个错误重试。
func (h *AdminPagesHandle) rolePermissionsDrawerData(c *gin.Context, roleID uint64, checkedOverride []uint64) (gin.H, error) {
	tree, err := h.roles.RolePermissionTree(c.Request.Context(), roleID)
	if err != nil {
		return nil, err
	}
	checkedIDs := tree.MenuIDs
	if checkedOverride != nil {
		checkedIDs = checkedOverride
	}
	checked := make(map[uint64]struct{}, len(checkedIDs))
	for _, id := range checkedIDs {
		checked[id] = struct{}{}
	}
	rows := make([]permTreeRow, 0, len(checked)+64)
	flattenPermissionTree(shell.TranslateFor(c), tree.Tree, 1, checked, &rows)
	return gin.H{"Role": tree, "Rows": rows}, nil
}

// RolePermissionsDrawer 角色权限分配的抽屉片段（GET /admin/roles/permissions/drawer?role_id=N）。
//
// 失败一律只给状态码、不给片段内容：drawer.js 对非 200（或形状不合）的响应会显示
// 「编辑表单加载失败，请重试」并把重试按钮聚焦 —— 这比服务端拼一个「半个片段」诚实，
// 因为缺 <form> 的片段会被它的 fragmentRoot 校验直接判非法，用户看到的还是同一个失败界面。
// 没有 role_id 就没有目标树（权限树只属于某一个角色），回角色列表并说明原因。
func (h *AdminPagesHandle) RolePermissionsDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	roleID := shell.ParseUint(c.Query("role_id"))
	if roleID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	data, err := h.rolePermissionsDrawerData(c, roleID, nil)
	if err != nil {
		// 角色不存在或树查询失败：记一条带 user_id 的结构化日志（原文只进日志），
		// 响应里不出现任何内部细节。
		adminErrParam(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	c.HTML(http.StatusOK, "admin/system/role_permissions.html", shell.Prepare(c, data))
}

// RolePermissionsSave 保存角色权限（POST /admin/roles/permissions/save）。
//
// 全量替换语义：提交上来的 menu_ids 就是该角色的完整授权集合，未勾选的一律撤销。
// **一个都没勾是合法提交**（清空该角色的全部权限），绝不能当成「没选，忽略本次提交」。
//
// 角色 id 与操作者都不信前端：role_id 走参数校验，操作者从会话注入（超管保护的判定依据，
// 见 RoleMenuSave）。menu_ids 由 service 侧过白名单（不在 sys_menus 里的 id 直接丢弃），
// 所以这里不做数量上限——请求方塞再多 id 也只经哈希查表，不进 SQL。
//
// 两条出口（与其它抽屉写表单同一套分档，见 templates/CLAUDE.md「写表单失败时原地留住输入」）：
//
//	· htmx 失败 → 200 + 片段自身（错误槽 + 用**本次提交的勾选**渲回的树），输入不丢；
//	· htmx 成功 → 200 + 成功态片段（就地回执 + 「关闭」），不刷新整页：
//	  列表页的角色行没有任何一列体现权限集合，没有可刷的新数据；
//	· 原生成功 → 303 回角色列表；原生失败 → 303 + ?err= 回角色列表。
//
// 不带 ?done= 回执：/admin/* 的 done 通道是受控文案白名单（adminDoneTexts），
// 里面只有批量结论与模块业务文案，成功文案被刻意排除（见 AdminFacingMessages 的注释）。
// 抽屉里那句回执走模板自身的成功态（admin.roles.perm.saved），不占用列表页的通道。
func (h *AdminPagesHandle) RolePermissionsSave(c *gin.Context) {
	roleID := shell.ParseUint(c.PostForm("role_id"))
	raw := c.PostFormArray("menu_ids")
	menuIDs := make([]uint64, 0, len(raw))
	for _, v := range raw {
		if id := shell.ParseUint(v); id != 0 {
			menuIDs = append(menuIDs, id)
		}
	}
	if roleID == 0 {
		// 缺 role_id：没有可回的抽屉（片段由 role_id 决定），回角色列表并说明原因。
		adminDrawerRedirect(c, adminPageErrURL("/admin/roles", response.TranslateMessage(c, adminenums.MsgBadRequest)))
		return
	}

	if _, err := h.roles.RoleMenuSave(c.Request.Context(), &admindto.RoleMenuSaveReq{
		RoleID:     roleID,
		MenuIDs:    menuIDs,
		OperatorID: shell.CurrentUserID(c),
	}); err != nil {
		msg := adminErrParam(c, err)
		data, treeErr := h.rolePermissionsDrawerData(c, roleID, menuIDs)
		if !adminDrawerHX(c) || treeErr != nil {
			// 原生提交（禁用 JS）：页面里没有就地放错误的位置，回列表并带 ?err=。
			// htmx 但树也取不回来时走同一出口 —— 此时只剩空树可渲，片段会退化成
			// 「没有可分配的菜单」的空态，那比一句通用错误更误导（像权限被清空了）。
			adminDrawerRedirect(c, adminPageErrURL("/admin/roles", msg))
			return
		}
		data["Err"] = msg
		c.HTML(http.StatusOK, "admin/system/role_permissions.html", shell.Prepare(c, data))
		return
	}

	if adminDrawerHX(c) {
		// 成功态片段只读 PermSaved 一个键（见模板）：不取树也不给列表数据，
		// 少一次查询，也避免「回执里那棵树的勾选」被误读成本次保存的结果快照。
		c.HTML(http.StatusOK, "admin/system/role_permissions.html", shell.Prepare(c, gin.H{"PermSaved": true}))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesCreate 新建角色（POST /admin/roles/create）。
func (h *AdminPagesHandle) RolesCreate(c *gin.Context) {
	roleCode := shell.FieldValue(c, "role_code")
	roleName := shell.FieldValue(c, "role_name")
	if roleCode == "" || roleName == "" {
		adminPageParamFail(c, "/admin/roles")
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.roles.RoleCreate(c.Request.Context(), &admindto.RoleCreateReq{
		RoleCode: roleCode, RoleName: roleName, Status: shell.ParseStatusPtr(c.PostForm("status")),
		SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/roles", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesUpdate 编辑角色（POST /admin/roles/update）。
func (h *AdminPagesHandle) RolesUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	roleName := shell.FieldValue(c, "role_name")
	if id == 0 || roleName == "" {
		adminPageParamFail(c, "/admin/roles")
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.roles.RoleUpdate(c.Request.Context(), &admindto.RoleUpdateReq{
		ID: id, RoleName: roleName, Status: shell.ParseStatus(c.PostForm("status")),
		SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/roles", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesDelete 删除角色（POST /admin/roles/delete）。
func (h *AdminPagesHandle) RolesDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, "/admin/roles")
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
		adminPageWriteFail(c, "/admin/roles", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/roles")
}

// RolesBulkDelete 批量删除角色（POST /admin/roles/bulk-delete）。
//
// 系统内置角色由 service 拒绝、其余照常删除；单条失败只计数不中断整批。
func (h *AdminPagesHandle) RolesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusSeeOther, "/admin/roles?err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	c.Redirect(http.StatusSeeOther, adminBulkResultURL(c, "/admin/roles", adminBulkNounRole, deleted, skipped))
}
