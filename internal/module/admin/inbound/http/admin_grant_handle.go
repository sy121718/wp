package adminhttp

// admin_grant_handle.go — 管理员的授权分配：角色抽屉 + 直接额外菜单抽屉。
//
// 落点说明：/api/admin/role/list|save 与 /api/admin/menu/list|save 早就存在 —— 权限点
// admin:role_list/save、admin:menu_list/save 在迁移 050 就 seed 了，service 侧还带超管保护 ——
// 但**此前没有任何界面调用方**：「这个管理员到底能用什么」只能靠直接改 sys_casbin_rule 完成。
//
// 两个抽屉对应 Casbin matcher（pkg/casbin/casbin.go）的两条来源，它们是并列的、不互相覆盖：
//   - 角色抽屉 → g 策略（user_id → role_code）：复用一整套权限；
//     matcher 的 `g(r.sub, p.sub) && g2(p.sub, "active")` 分支；
//   - 菜单抽屉 → p 策略（sub = user_id）：这个人额外需要的那几个入口，
//     matcher 的 `r.sub == p.sub` 直连分支 —— 也就是「菜单算额外权限」。
//
// 为什么不合成一棵「有效权限树」：一条是「换一套权限」，一条是「补几个入口」，
// 混在一起后「取消勾选到底动了哪一层」就不可解释了（继承来的权限在这里根本删不掉，
// 只能去角色里撤销）。菜单抽屉因此把继承项显式标出来并禁用勾选。
//
// 两条保存都是**全量替换**语义（Casbin 侧就是 Replace*）：提交上来的集合就是最终状态，
// 没勾的一律撤销。一个都没勾是合法提交（等于「收回全部授权」），绝不能当成「没选，忽略」。

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
	"go_wp/pkg/response"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
)

// --- 角色分配 ---

// adminRoleChoice 角色抽屉里的一行。
//
// Checked 在 Go 侧算好再交给模板（模板只渲染、不做判断），与 permTreeRow.Checked 同一取舍。
type adminRoleChoice struct {
	Code   string
	Name   string
	Status int
	// Missing 表示「这个账号绑定的角色码已经不在角色表里」（角色被删或禁用后 g 策略还在）。
	// 不把它渲染出来的后果与角色分权里的禁用项完全一样：管理员一保存就把这条绑定静默删掉了，
	// 而界面上什么都没显示过（见 menu_authz.go 的 buildPermissionTree 注释）。
	Missing bool
	// Checked 该账号当前绑定了这个角色。
	Checked bool
}

// AdministratorRolesDrawer 管理员角色分配的抽屉片段（GET /admin/administrators/roles/drawer?user_id=N）。
//
// 失败一律只给状态码（drawer.js 对非 200 显示「加载失败，请重试」并聚焦重试按钮）：
// 缺 <form> 的半截片段会被它的 fragmentRoot 校验判非法，用户看到的还是同一个失败界面。
func (h *AdminPagesHandle) AdministratorRolesDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID := shell.ParseUint(c.Query("user_id"))
	if userID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	data, err := h.administratorRolesDrawerData(c, userID, nil)
	if err != nil {
		adminErrParam(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	c.HTML(http.StatusOK, "admin/system/admin_role_drawer.html", shell.Prepare(c, data))
}

// administratorRolesDrawerData 装配角色抽屉：候选角色（全量）+ 该账号当前绑定的 codes。
//
// checkedOverride 非 nil 时用它代替库里的绑定集合 —— 保存失败路径要用本次提交的值重渲染，
// 用库里的值会把用户刚勾的那一屏整块回退（与角色分权抽屉同一处置）。
func (h *AdminPagesHandle) administratorRolesDrawerData(c *gin.Context, userID uint64, checkedOverride []string) (gin.H, error) {
	ctx := c.Request.Context()
	account, err := h.admins.AdminDetail(ctx, &admindto.AdminDetailReq{Id: userID})
	if err != nil {
		return nil, err
	}
	if account == nil || account.ID != userID {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	all, err := h.listAllRoles(ctx)
	if err != nil {
		return nil, err
	}
	bound, err := h.admins.AdminRoleList(ctx, &admindto.AdminRoleListReq{UserID: userID})
	if err != nil {
		return nil, err
	}

	checked := make(map[string]struct{}, len(bound.List))
	for _, item := range bound.List {
		checked[item.RoleCode] = struct{}{}
	}
	if checkedOverride != nil {
		checked = make(map[string]struct{}, len(checkedOverride))
		for _, code := range checkedOverride {
			if code != "" {
				checked[code] = struct{}{}
			}
		}
	}

	rows := make([]adminRoleChoice, 0, len(all)+len(checked))
	seen := make(map[string]struct{}, len(all))
	for _, role := range all {
		_, on := checked[role.RoleCode]
		seen[role.RoleCode] = struct{}{}
		rows = append(rows, adminRoleChoice{Code: role.RoleCode, Name: role.RoleName, Status: role.Status, Checked: on})
	}
	// 已绑定但不在候选里的码：照样渲染成一行（标记为未知），否则保存即静默解除绑定。
	for code := range checked {
		if _, ok := seen[code]; ok {
			continue
		}
		rows = append(rows, adminRoleChoice{Code: code, Checked: true, Missing: true})
	}
	// 未知角色排在末尾：它们的顺序不稳定（map 迭代），排在前面会让每次打开抽屉的清单都不一样。
	sortRoleChoices(rows)

	return gin.H{"Account": account, "Rows": rows}, nil
}

// listAllRoles 取全部角色（角色列表接口每页上限 100，循环取完）。
//
// 不直接 limit=100：抽屉里的候选数量不该由一个分页上限决定 —— 角色超过 100 个时
// 会静默少几个候选，而管理员只会觉得「那个角色怎么没了」。
func (h *AdminPagesHandle) listAllRoles(ctx context.Context) ([]admindto.RoleItem, error) {
	const pageSize = 100
	out := make([]admindto.RoleItem, 0, pageSize)
	for page := 1; page <= 50; page++ {
		res, err := h.roles.RoleList(ctx, &admindto.RoleListReq{
			RolePageReq: admindto.RolePageReq{Page: page, Limit: pageSize},
		})
		if err != nil {
			return nil, err
		}
		if res == nil || len(res.List) == 0 {
			break
		}
		out = append(out, res.List...)
		if int64(len(out)) >= res.Total {
			break
		}
	}
	return out, nil
}

// sortRoleChoices 已知角色按 status 降序 / code 升序，未知角色（Missing）恒排最后。
func sortRoleChoices(rows []adminRoleChoice) {
	// 未知项数量通常为 0，用一次分区代替引入 sort 包的比较函数（可读性优先）。
	known := make([]adminRoleChoice, 0, len(rows))
	missing := make([]adminRoleChoice, 0, 2)
	for _, r := range rows {
		if r.Missing {
			missing = append(missing, r)
			continue
		}
		known = append(known, r)
	}
	copy(rows, known)
	copy(rows[len(known):], missing)
}

// AdministratorRolesSave 保存管理员的角色绑定（POST /admin/administrators/roles/save）。
//
// 操作者从会话注入（超管保护的判定依据，见 AdminRoleSave 的注释），请求体里的角色码不被信任：
// service 侧会把它们交给 Casbin 全量替换，而 Casbin 侧的 g2 策略只认「角色是否启用」，
// 不认「这个角色码是否真的存在于 sys_role」—— 所以这里不做存在性预筛也会被服务端拒绝或忽略。
func (h *AdminPagesHandle) AdministratorRolesSave(c *gin.Context) {
	userID := shell.ParseUint(c.PostForm("user_id"))
	if userID == 0 {
		adminDrawerRedirect(c, adminPageErrURL("/admin/administrators", response.TranslateMessage(c, adminenums.MsgBadRequest)))
		return
	}
	codes := make([]string, 0, 8)
	for _, v := range c.PostFormArray("role_codes") {
		if v = strings.TrimSpace(v); v != "" {
			codes = append(codes, v)
		}
	}
	if _, err := h.admins.AdminRoleSave(c.Request.Context(), &admindto.AdminRoleSaveReq{
		UserID: userID, RoleCodes: codes, OperatorID: shell.CurrentUserID(c),
	}); err != nil {
		msg := adminErrParam(c, err)
		data, loadErr := h.administratorRolesDrawerData(c, userID, codes)
		if !adminDrawerHX(c) || loadErr != nil {
			adminDrawerRedirect(c, adminPageErrURL("/admin/administrators", msg))
			return
		}
		data["Err"] = msg
		c.HTML(http.StatusOK, "admin/system/admin_role_drawer.html", shell.Prepare(c, data))
		return
	}
	if adminDrawerHX(c) {
		c.HTML(http.StatusOK, "admin/system/admin_role_drawer.html", shell.Prepare(c, gin.H{"Saved": true}))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}

// --- 直接额外菜单权限 ---

// adminMenuTreeRow 菜单抽屉的展平行。
//
// 与 permTreeRow 分开而不是复用它：两者渲染的信息不同 —— 这里的每一行还要回答
// 「这个勾是直接给的，还是角色给的」，复用一个结构会让「哪些字段只对其中一处有意义」
// 变得不可判读。展平方式（DFS + depth + PadLeft）与角色分权一致，理由见 flattenPermissionTree。
//
// 与同包的 adminMenuRow / flattenAdminMenuTree 也不是同一个东西：那一份是**菜单管理视角**
// 的展平（路径 / 状态 / 排序 / 备注，供列表页用），本函数是**授权视角**（勾选态 + 继承标记）。
// 两者的字段没有交集，不要为了「少一个函数」把它们合并。
type adminMenuTreeRow struct {
	ID        uint64
	Title     string
	Type      int
	TypeLabel string
	Depth     int
	PadLeft   int
	ParentID  uint64
	// Checked 该行显示为已授权（直接授权或角色继承）。
	Checked bool
	// Inherited 来自角色继承：显示为已授权但**禁用勾选框** —— 在这里取消它不会生效
	// （权限来自角色的 p 策略，与这条直接授权无关），给一个「点了没反应」的勾选框
	// 比不给更糟。要撤销请去角色抽屉或角色的「权限分配」。
	Inherited   bool
	HasChildren bool
}

// AdministratorMenusDrawer 管理员直接额外菜单的抽屉片段（GET /admin/administrators/menus/drawer?user_id=N）。
func (h *AdminPagesHandle) AdministratorMenusDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID := shell.ParseUint(c.Query("user_id"))
	if userID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	data, err := h.administratorMenusDrawerData(c, userID, nil)
	if err != nil {
		adminErrParam(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	c.HTML(http.StatusOK, "admin/system/admin_menu_drawer.html", shell.Prepare(c, data))
}

// administratorMenusDrawerData 装配菜单抽屉：菜单树 + 直接授权集合 + 有效集合（含角色继承）。
//
// directOverride 非 nil 时用它代替库里的直接授权集合（保存失败回显）：
// 回显要显示「用户刚勾的那一屏」，同时继承集合里的直接部分要换成本次提交的集合 ——
// 否则用户刚取消的勾选会以「来自角色」的样子重新变成已授权，而他正需要在这里改掉。
func (h *AdminPagesHandle) administratorMenusDrawerData(c *gin.Context, userID uint64, directOverride []uint64) (gin.H, error) {
	ctx := c.Request.Context()
	account, err := h.admins.AdminDetail(ctx, &admindto.AdminDetailReq{Id: userID})
	if err != nil {
		return nil, err
	}
	if account == nil || account.ID != userID {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	trees, err := h.menus.MenuTree(ctx, &admindto.MenuTreeReq{})
	if err != nil {
		return nil, err
	}
	granted, err := h.admins.AdminMenuList(ctx, &admindto.AdminMenuListReq{UserID: userID})
	if err != nil {
		return nil, err
	}

	direct := idSet(granted.DirectMenuIDs)
	effective := idSet(granted.EffectiveMenuIDs)
	roleOnly := make(map[uint64]struct{}, len(effective))
	for id := range effective {
		if _, ok := direct[id]; !ok {
			roleOnly[id] = struct{}{}
		}
	}
	if directOverride != nil {
		direct = idSet(directOverride)
	}
	for id := range direct {
		effective[id] = struct{}{}
	}
	for id := range roleOnly {
		effective[id] = struct{}{}
	}

	rows := make([]adminMenuTreeRow, 0, 128)
	flattenGrantMenuTree(shell.TranslateFor(c), trees, 1, direct, effective, &rows)
	return gin.H{"Account": account, "Rows": rows}, nil
}

// idSet 把 id 列表转成集合（判据里大量使用「在不在集合里」）。
func idSet(ids []uint64) map[uint64]struct{} {
	out := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

// flattenGrantMenuTree 深度优先展平菜单树，勾选态由「直接授权 ∪ 角色继承」决定。
//
// 展平顺序必须是 DFS：页面的折叠与搜索靠「同一父级的子树在数组里连续」这一性质实现，
// 顺序一旦打乱，折叠会藏错行（与角色分权同一约束，见 flattenPermissionTree）。
func flattenGrantMenuTree(tr adminLabelTranslate, nodes []admindto.MenuTreeNode, depth int, direct, effective map[uint64]struct{}, out *[]adminMenuTreeRow) {
	for _, n := range nodes {
		_, isDirect := direct[n.ID]
		_, isEffective := effective[n.ID]
		*out = append(*out, adminMenuTreeRow{
			ID: n.ID, Title: n.Title, Type: n.Type, TypeLabel: adminMenuTypeLabel(tr, n.Type),
			Depth: depth, PadLeft: (depth-1)*permRowIndentStep + 10, ParentID: n.ParentID,
			Checked: isDirect || isEffective, Inherited: !isDirect && isEffective,
			HasChildren: len(n.Children) > 0,
		})
		if len(n.Children) > 0 {
			flattenGrantMenuTree(tr, n.Children, depth+1, direct, effective, out)
		}
	}
}

// AdministratorMenusSave 保存管理员的直接额外菜单权限（POST /admin/administrators/menus/save）。
//
// 提交的是「直接授权」那一层：角色继承来的项在抽屉里是禁用的，浏览器不会提交它们，
// 所以把当前提交集合整体当作直接授权集合是准确的（service 侧同样按全量替换处理）。
// 操作者从会话注入 —— 直接权限写入覆盖全部权限点时等价于造超管，这个判定只认会话。
func (h *AdminPagesHandle) AdministratorMenusSave(c *gin.Context) {
	userID := shell.ParseUint(c.PostForm("user_id"))
	if userID == 0 {
		adminDrawerRedirect(c, adminPageErrURL("/admin/administrators", response.TranslateMessage(c, adminenums.MsgBadRequest)))
		return
	}
	raw := c.PostFormArray("menu_ids")
	menuIDs := make([]uint64, 0, len(raw))
	for _, v := range raw {
		if id := shell.ParseUint(v); id != 0 {
			menuIDs = append(menuIDs, id)
		}
	}
	if _, err := h.admins.AdminMenuSave(c.Request.Context(), &admindto.AdminMenuSaveReq{
		UserID: userID, MenuIDs: menuIDs, OperatorID: shell.CurrentUserID(c),
	}); err != nil {
		msg := adminErrParam(c, err)
		data, loadErr := h.administratorMenusDrawerData(c, userID, menuIDs)
		if !adminDrawerHX(c) || loadErr != nil {
			adminDrawerRedirect(c, adminPageErrURL("/admin/administrators", msg))
			return
		}
		data["Err"] = msg
		c.HTML(http.StatusOK, "admin/system/admin_menu_drawer.html", shell.Prepare(c, data))
		return
	}
	if adminDrawerHX(c) {
		c.HTML(http.StatusOK, "admin/system/admin_menu_drawer.html", shell.Prepare(c, gin.H{"Saved": true}))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/administrators")
}
