package adminhttp

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

// 以及跨领域共用的抽屉分流与取词辅助。
//
// 六个领域的页面实现按能力域拆在同包兄弟文件（外部贡献者导航）：
//   · admin_pages_admin.go      管理员              /admin/administrators
//   · admin_pages_role.go       角色（含权限分配抽屉）/admin/roles
//   · admin_pages_perm.go       权限点              /admin/permissions
//   · admin_pages_menu.go       菜单（树 + 绑定权限点）/admin/menus
//   · admin_pages_dept.go       部门（树）          /admin/departments
//   · admin_pages_datarule.go   数据权限            /admin/datarules
//   · admin_pages_i18n.go       文案词条页          /admin/i18n
//   · admin_pages_lang.go       后台语言切换        /admin/lang
//   · admin_pages_login.go      后台登录页          /admin/login
//
// 这些管理页自 dashboard 模块搬回 admin（页面住在哪个模块就归哪个模块）：严格跨模块边界在此消失，
// 页面直接持有本模块 service 契约（同包语义），不再绕 dashboard。页面文案标题沿用原有 i18n 词条 key
// （与 dashboard 副本逐字一致，保证渲染不变）。

// 表单约定（组序号 = g，行序 = 行在数组中的位置；同名 input 重复出现即被浏览器提交为数组）：
//
//	config_editor            隐藏标记：出现即表示本次提交带完整配置（否则沿用库中原配置）
//	omit_fields              多选：查询结果中屏蔽的字段（限于该域白名单）
//	groups[g].logic          条件组 g 的组合逻辑（AND / OR）
//	groups[g].conditions[i].field / .op / .value
//	                         条件组 g 的第 i 行（字段 / 操作符 / 值）
//
// 「添加 / 删除行与组、切换字段」都打回同一个片段端点：服务端从提交的表单值重建当前配置、
// 应用动作、再按该域白名单渲染整块编辑器。当前编辑到一半的内容始终由表单承载 ——
// 服务端没有会话状态，前端没有一行 JS（与 product_attribute_rows 同型）。
//
// 服务端只解析、不判断；字段是否属于该域、操作符是否被该字段声明，仍由 service 的
// validateRuleConfig 按域声明判定（表单下拉只是让人不容易填错，不是校验）。

// ## 为什么要它
//
// 后台每个页面都要登录，而登录要过验证码 —— 人肉点一次还行，用浏览器做端到端验证时
// 每次会话重建都要重来一遍。于是有了这个入口：
//
//	http://127.0.0.1:8080/admin/dev-login?to=/admin/mail/automation/canvas?id=1
//
// ## 四道锁（这是后门形状的东西，必须锁死）
//
//  1. **release 模式根本不注册这个路由**（见 routers/routes.go），不是「注册了再判断」。
//     不存在的路由无法被利用，比运行时 if 更可靠。
//  2. **只认环回地址**：用 c.Request.RemoteAddr 而不是 c.ClientIP() —— 后者会被
//     X-Forwarded-For 影响，代理配置不当就能从外部伪造出一个「本机请求」。
//  3. **只允许登录超管**（is_admin=1）：不会拿它去登普通管理员，避免越权面。
//  4. **走与正常登录完全同一条会话路径**（auth.NewSessionID + SaveUserSession +
//     RefreshOnline + RotateCSRFToken）：不做「绕过校验直接塞 cookie」的旁路，
//     否则开发环境的行为与生产不一致，验证也失去意义。每次使用都写日志。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/admin/contract"
	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/internal/module/admin/service"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/auth"
	"go_wp/pkg/captcha"
	"go_wp/pkg/datarule"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
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
		adminAdminsPage.paramFail(c)
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
			adminAdminsPage.fail(c, msg)
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
	adminAdminsPage.done(c)
}

// --- 直接额外菜单权限 ---

// adminMenuTreeRow 菜单抽屉的展平行。
//
// 与 permTreeRow 分开而不是复用它：两者渲染的信息不同 —— 这里的每一行还要回答
// 「这个勾是直接给的，还是角色给的」，复用一个结构会让「哪些字段只对其中一处有意义」
// 变得不可判读。展平方式（DFS + depth + PadLeft）与角色分权一致，理由见 flattenPermissionTree。
//
// 与菜单管理页的展平（service.MenuPage 的 flattenMenuPageRows + 列表模板）也不是同一个东西：
// 那一份是**菜单管理视角**（路径 / 状态 / 排序 / 备注，供列表页用，且要带折叠态），
// 本函数是**授权视角**（勾选态 + 继承标记）。
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
		adminAdminsPage.paramFail(c)
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
			adminAdminsPage.fail(c, msg)
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
	adminAdminsPage.done(c)
}

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
		// 降级渲染：res 换零值、页面照常渲染（详见 adminListLoadErr 的说明）。
		res = &admindto.AdminListResp{}
	}
	data := shell.Prepare(c, gin.H{
		"title":       pagesMsgAdministratorsTitle,
		"menu":        "admins",
		"Rows":        res.List,
		"Total":       res.Total,
		"FilterName":  name,
		"FilterEmail": email,
		"Err":         adminListLoadErr(c, err),
		"ListQuery":   adminAdminsPage.listQuery(c),
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
		adminAdminsPage.paramFail(c)
		return
	}
	if _, err := h.admins.AdminCreate(c.Request.Context(), &admindto.AdminCreateReq{
		Username: username, Email: email, Password: password,
		Phone: shell.FieldValue(c, "phone"), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminAdminsPage.writeFail(c, err)
		return
	}
	adminAdminsPage.done(c)
}

// AdministratorsUpdate 编辑管理员（POST /admin/administrators/update）。
// AdminEdit 仅更新 username/phone/email/remark。
func (h *AdminPagesHandle) AdministratorsUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminAdminsPage.paramFail(c)
		return
	}
	if _, err := h.admins.AdminEdit(c.Request.Context(), &admindto.AdminEditReq{
		Id: id, Username: shell.FieldValue(c, "username"), Phone: shell.FieldValue(c, "phone"),
		Email: shell.FieldValue(c, "email"), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminAdminsPage.writeFail(c, err)
		return
	}
	adminAdminsPage.done(c)
}

// AdministratorsDelete 删除管理员（POST /admin/administrators/delete）。
//
// OperatorID 必须从会话注入：AdminDelete 的「不能删自己」判定依赖它，缺省 0 时该判定
// 静默失效（只剩超管保护兜底）—— 单条端点此前漏了注入，而批量端点从一开始就带，
// 于是出现「批量比单条更严」的错位。
func (h *AdminPagesHandle) AdministratorsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminAdminsPage.paramFail(c)
		return
	}
	if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{
		Id: []uint64{id}, OperatorID: shell.CurrentUserID(c),
	}); err != nil {
		adminAdminsPage.writeFail(c, err)
		return
	}
	adminAdminsPage.done(c)
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
		// 超限是受控错误（理由见「六领域公共：批量动作」一节）：文案走 shell 的受控出口，不直传原文。
		adminAdminsPage.fail(c, shell.BulkIDsFacingText(c, berr))
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
	ok, msg := adminBulkOutcome(c, adminBulkNounAdmin, deleted, skipped)
	adminAdminsPage.jump(c, ok, msg)
}

// --- 数据权限 datarules ---

// DatarulesPage 数据权限列表页（GET /admin/datarules）。
// 列表 + 新建（domain 下拉来自已注册数据域）+ 删除；编辑走独立 /edit?id= 页（detail 回显，配置复杂）。
//
// 服务端筛选：domain 进 SQL（与分页同源）；回显键名对齐模板 datarules.html 的
// value="{{.["FilterDomain"]}}"。注意这是**精确匹配**（service 侧 `domain = ?`）：
// 输入的真实域值必须与 sys_data_rule.domain 完全一致（如 ADMIN），
// 部分串不会命中 —— 改动匹配语义在 service，不在本页。
func (h *AdminPagesHandle) DatarulesPage(c *gin.Context) {
	page, limit := shell.PageParams(c)
	domain := strings.TrimSpace(c.Query("domain"))
	res, err := h.rules.RuleList(c.Request.Context(), &admindto.RuleListReq{
		Page: page, Limit: limit, Domain: domain,
	})
	if err != nil {
		res = &admindto.RuleListResp{}
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	data := shell.Prepare(c, gin.H{
		"title":        pagesMsgDatarulesTitle,
		"menu":         "datarules",
		"Rows":         res.List,
		"Total":        res.Total,
		"Domains":      domains,
		"FilterDomain": domain,
		"Err":          adminListLoadErr(c, err),
		"ListQuery":    adminDatarulesPage.listQuery(c),
	})
	base := shell.FilterBaseURL("/admin/datarules", map[string]string{"domain": domain})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/datarules", data)
}

// DatarulesCreate 新建数据规则（POST /admin/datarules/create）。
//
// 创建只收基础字段：规则配置在编辑页按该数据域的白名单逐项填写 —— 列表页的抽屉表单是
// 静态模板、拿不到白名单，让它在没有字段清单的情况下收配置等于把 JSON 换了个地方手写。
func (h *AdminPagesHandle) DatarulesCreate(c *gin.Context) {
	ruleName := shell.FieldValue(c, "rule_name")
	domain := shell.FieldValue(c, "domain")
	if ruleName == "" || domain == "" {
		adminDatarulesPage.paramFail(c)
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: admindto.RuleConfigDTO{},
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminDatarulesPage.writeFail(c, err)
		return
	}
	adminDatarulesPage.done(c)
}

// DatarulesEditPage 数据规则编辑页（GET /admin/datarules/edit?id=X）。
// 从 RuleDetail 回显 rule_name/domain/status/remark 与 config JSON。
func (h *AdminPagesHandle) DatarulesEditPage(c *gin.Context) {
	id := shell.ParseUint(c.Query("id"))
	if id == 0 {
		// 页面请求的失败出口是**提示页**（shell.RenderJump）：原先是
		// c.String(400, pagesMsgFieldRequired) —— 响应体是 i18n 的 key 本身
		//（用户看到内部标识符），而且脱离页壳。现在渲染整页提示 + 回列表页的链接。
		adminDatarulesPage.paramFail(c)
		return
	}
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil || detail == nil {
		// id 存在但查不到（已被别人删掉 / 不在本工程作用域）：与上面「缺 id」同一形状的失败 ——
		// 也不能直出裸文本。原先是 c.String(404, "数据规则不存在")：响应体是那句中文、
		// **没有页壳**，用户在编辑页上点了半天链接后落到一个纯文本页面。
		// 文案取 adminenums.ErrRuleNotFound（在 AdminFacingMessages 白名单里，过归口助手）。
		adminDatarulesPage.writeFail(c, errors.New(adminenums.ErrRuleNotFound))
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	// 条件编辑器按该数据域的白名单渲染（字段 / 操作符下拉都来自域声明）。
	editor := h.dataruleEditorContext(c, detail.Domain, detail.Config)
	// 保存失败不再回本页并带 ?err=（写动作的结论走整页提示），本页因此没有错误槽位。
	c.HTML(http.StatusOK, "admin/system/datarule_edit", shell.Prepare(c, gin.H{
		"title":   pagesMsgDatarulesTitle,
		"menu":    "datarules",
		"Detail":  detail,
		"Domains": domains,
		"Editor":  editor,
	}))
}

// adminDataruleBack /admin/datarules/update 的失败回跳目标：按提交**来源**分流。
//
// 这是本域唯一「一个端点两个入口」的写操作 —— 列表页的抽屉表单（datarules.html，只改
// rule_name/domain/status/remark）与编辑页的完整表单（datarule_edit.html，还带条件配置）
// 都 POST 到 /admin/datarules/update。分流依据**本来就有**：编辑页提交时带
// dataruleEditorMarker（config_editor 隐藏域，见 datarule_config_form.go 的表单约定），
// 抽屉不带 —— 用它判来源，不新增协议、不猜别的字段。
//
// 缺 id（0）时没有可回的编辑页（它由 id 决定），一律回列表页。
func adminDataruleBack(c *gin.Context, id uint64) (back, backText string) {
	if id == 0 || c.PostForm(dataruleEditorMarker) == "" {
		return adminDatarulesPage.back(c), adminDatarulesPage.backText(c)
	}
	back = shell.WithParams("/admin/datarules/edit", map[string]string{"id": strconv.FormatUint(id, 10)})
	// 编辑页那份表单填一次成本很高，失败提示页上的链接必须指回它（而不是列表页）。
	return back, shell.TranslateFor(c)("admin.datarules.back_to_edit", "返回编辑页")
}

// DatarulesUpdate 保存数据规则（POST /admin/datarules/update）。
//
// 配置来源按提交内容判定：带编辑器标记的（编辑页）从表单重建配置，否则沿用库中原配置 ——
// 列表抽屉只改基础字段，不该把规则配置清空。空行与空条件组在这里被清掉：字段为空的行会被
// 引擎直接丢弃、值为空的行会变成 field = ”，两种都会让界面上的条数与实际生效的条数对不上。
func (h *AdminPagesHandle) DatarulesUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	ruleName := shell.FieldValue(c, "rule_name")
	domain := shell.FieldValue(c, "domain")
	// 失败出口回**来源页**（判定依据见 adminDataruleBack）：编辑页提交就回编辑页，
	// 抽屉提交就回列表页 —— 编辑页那份表单填一次成本很高，甩回列表页等于让他重填。
	back, backText := adminDataruleBack(c, id)
	if id == 0 || ruleName == "" || domain == "" {
		adminJump(c, false, response.TranslateMessage(c, adminenums.MsgBadRequest), back, backText)
		return
	}
	config := admindto.RuleConfigDTO{}
	if c.PostForm(dataruleEditorMarker) != "" {
		config = dataruleDropEmptyGroups(dataruleConfigFromForm(c))
	} else {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil {
			adminJump(c, false, adminErrParam(c, detailErr), back, backText)
			return
		}
		if detail == nil {
			// 「查不到但也没报错」必须给一条可行动的业务文案（规则不存在 / 不在本工程作用域内），
			// 不能像修复前那样拿 nil 去调错误出口（旧出口对 nil 直接 return → 200 空体，
			// 前端看到「点了没反应」而日志里什么都没有）。
			adminJump(c, false, adminErrParam(c, errors.New(adminenums.ErrRuleNotFound)), back, backText)
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminJump(c, false, adminErrParam(c, err), back, backText)
		return
	}
	adminDatarulesPage.done(c)
}

// DatarulesDelete 删除数据规则（POST /admin/datarules/delete）。
func (h *AdminPagesHandle) DatarulesDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminDatarulesPage.paramFail(c)
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		adminDatarulesPage.writeFail(c, err)
		return
	}
	adminDatarulesPage.done(c)
}

// DatarulesBulkDelete 批量删除数据规则（POST /admin/datarules/bulk-delete）。
//
// 逐条走同一条单条删除路径（含该规则的分配记录清理）；单条失败只计数不中断整批。
func (h *AdminPagesHandle) DatarulesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见「六领域公共：批量动作」一节）：文案走 shell 的受控出口，不直传原文。
		adminDatarulesPage.fail(c, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := adminBulkOutcome(c, adminBulkNounDatarule, deleted, skipped)
	adminDatarulesPage.jump(c, ok, msg)
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
		"Err":           adminListLoadErr(c, err),
		"ListQuery":     adminDeptsPage.listQuery(c),
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
		adminDeptsPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.depts.DeptCreate(c.Request.Context(), &admindto.DeptCreateReq{
		ParentID: shell.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminDeptsPage.writeFail(c, err)
		return
	}
	adminDeptsPage.done(c)
}

// DepartmentsUpdate 编辑部门（POST /admin/departments/update）。
func (h *AdminPagesHandle) DepartmentsUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	deptName := shell.FieldValue(c, "dept_name")
	deptCode := shell.FieldValue(c, "dept_code")
	if id == 0 || deptName == "" || deptCode == "" {
		adminDeptsPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.depts.DeptUpdate(c.Request.Context(), &admindto.DeptUpdateReq{
		ID: id, ParentID: shell.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminDeptsPage.writeFail(c, err)
		return
	}
	adminDeptsPage.done(c)
}

// DepartmentsDelete 删除部门（POST /admin/departments/delete）。
func (h *AdminPagesHandle) DepartmentsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminDeptsPage.paramFail(c)
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
		adminDeptsPage.writeFail(c, err)
		return
	}
	adminDeptsPage.done(c)
}

// DepartmentsBulkDelete 批量删除部门（POST /admin/departments/bulk-delete）。
//
// 有子部门或部门下有管理员的由 service 拒绝、其余照常删除；单条失败只计数不中断整批。
func (h *AdminPagesHandle) DepartmentsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见「六领域公共：批量动作」一节）：文案走 shell 的受控出口，不直传原文。
		adminDeptsPage.fail(c, shell.BulkIDsFacingText(c, berr))
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
	ok, msg := adminBulkOutcome(c, adminBulkNounDept, deleted, skipped)
	adminDeptsPage.jump(c, ok, msg)
}

// 页面标题与统一提示（沿用原 i18n 词条 key，词条缺失时前端回退中文注释值）。
const (
	pagesMsgAdministratorsTitle = "MsgAdministratorsTitle" // 管理员
	pagesMsgRolesTitle          = "MsgRolesTitle"          // 角色管理
	pagesMsgMenusTitle          = "MsgMenusTitle"          // 菜单管理
	pagesMsgPermissionsTitle    = "MsgPermissionsTitle"    // 权限资源
	pagesMsgDepartmentsTitle    = "MsgDepartmentsTitle"    // 部门管理
	pagesMsgDatarulesTitle      = "MsgDatarulesTitle"      // 数据权限
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

// --- 六领域公共：批量动作 ---
//
// 批量删除一律「逐条走同一条单条删除路径 + 单条失败不中断整批」：一条被服务端拒绝
// （删自己或超管、系统角色、已分配的权限点、有子级的菜单 / 部门）不能把整批回滚 ——
// 那会让人以为「一条都没删」，然后反复重试。结果按「已删除 N 个 / M 个未能删除」
// 回带列表页，避免静默的部分成功。权限点复用各自单条删除的业务 API，不新增权限点。

// 超限（berr）为什么可以展示、而 service 错误必须走 adminErrParam：
// berr 来自 shell.BulkIDs，它是**带 sentinel 的类型**（shell.BulkIDsError，值域只有 Count/Max
// 两个整数）——「受控」因此是类型事实，而不是注释里的人肉判断。
// 展示一律走 shell.BulkIDsFacingText：它只认类型、按当前语言拼出
// 「一次最多操作 N 项，当前 M 项，请分批进行」，这句可行动提示不会被归口助手抹成通用提示
// （那正是本轮要避免的反向缺陷）；service 返回的 err 值域包含 SQLSTATE，所以走助手。
//
// 结论的成品文案在 admin_err.go（adminBulkOutcome / adminBulkTextOf），出口在 admin_jump.go
//（整页提示）。页面写操作的失败出口同样在 admin_jump.go：参数级 → adminPageRef.paramFail、
// service 错误 → adminPageRef.writeFail —— 结论不再经查询参数回带，所以原先那一整段
// 「303 + ?err= 的构造与读侧白名单」已随传输通道整批删除。

// adminLabelTranslate 展示层取词函数（与 shell.TranslateFor(c) 同形，可直接传它）。
type adminLabelTranslate = func(key, fallback string) string

// adminMenuTypeLabel 菜单类型 → 当前语言的展示名。
//
// 取值映射的真源在 adminenums.MenuTypeLabel（key 与后台模板的 tr("admin.menus.type.*")
// 是同一批）；这里只负责取词。key 为空（iframe 这类技术名词）直接用兜底。
func adminMenuTypeLabel(tr adminLabelTranslate, t int) string {
	key, fallback := adminenums.MenuTypeLabel(t)
	if tr == nil || strings.TrimSpace(key) == "" {
		return fallback
	}
	return tr(key, fallback)
}

func adminDrawerHX(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// --- 文案词条页（审计 I18N-003） ---
//
// 迁移 seed 是默认值来源（ON CONFLICT DO NOTHING，不覆盖这里的修改）；
// 本页是真相来源（运营改过之后，seed 不会再回来覆盖）。
// 保存/删除后立即重载缓存（pkg/i18n 内部完成）。

// adminI18nEntryPageSize 每页条数。
const adminI18nEntryPageSize = 50

// adminI18nEntryHandle 词条页处理器（读写走 pkg/i18n 的端口，失效走装配注入的页面标记）。
type adminI18nEntryHandle struct {
	// pages 词条变更后标记站点待重建（装配期注入）。
	//
	// 为什么必须接：sys_i18n 的词条在**构建期**取词并烘进 HTML 字节（组件固定文案），
	// 改了词条而没人标 stale，站点就永远输出旧文案 —— 而且没有任何报错。
	// 页面 / 商品 / 导航翻译工作台与站点设置这四条路径历来都调 page.MarkStaleForI18n，
	// 词条页此前漏了这一环（词条是全局的：sys_i18n 没有工程维度，所以没有"精确到某页"
	// 的选项，一律按 i18n:site 依赖做全站标记）。
	pages adminI18nPageMarker
	// dict 字典只读口（sysconfig 契约，装配期注入）：给语言筛选下拉与「新建词条」的
	// 语言下拉供数。此前两处各写死 zh-CN / en-US 两条 option —— 于是**已收录的其它语言
	// 在页面上完全不可选**，建词条只能靠手输 URL 里的参数。
	//
	// 用不用 ui_available 做区分：**用**，但只在「新建词条」那一处 —— 那一处的下一步
	// 问题是「我该给哪个语言补词条」，标记出哪些语种已有界面译文正好回答它；
	// 筛选下拉不加标记（筛的是已有词条，标记只是噪音）。
	dict sysconfigcontract.DictReader
}

// adminI18nPageMarker 页面侧的最小失效端口（消费者侧定义，跨模块只依赖这一条）。
type adminI18nPageMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

// SetPageMarker 注入页面失效端口（装配期）。
func (h *adminI18nEntryHandle) SetPageMarker(m adminI18nPageMarker) { h.pages = m }

// SetDictReader 注入字典只读口（装配期）：语言下拉的供数来源。
func (h *adminI18nEntryHandle) SetDictReader(r sysconfigcontract.DictReader) { h.dict = r }

// NewAdminI18nEntryHandle 构造。
func NewAdminI18nEntryHandle() *adminI18nEntryHandle { return &adminI18nEntryHandle{} }

// markI18nStale 词条变更后标记站点待重建。
//
// 失败只记日志、**不改变响应**：词条此刻已经写进库了，回报"保存失败"会让运营以为没保存
// 而反复重试；而站点停在旧文案是**可见**的降级（下次编辑 / 发布会自然覆盖）。
// 未注入端口时直接返回 —— 装配漏接的表现是"改了词条站点不更新"，由 wiring 端口清单兜底。
func (h *adminI18nEntryHandle) markI18nStale(c *gin.Context) {
	if h == nil || h.pages == nil {
		return
	}
	if err := h.pages.MarkStaleForI18n(c.Request.Context()); err != nil {
		logger.Scene(adminErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "词条变更后标记站点待重建失败")
	}
}

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
	editURLs := make([]string, len(items))
	for idx, entry := range items {
		query := url.Values{"key": {entry.Key}, "lang": {entry.Lang}}
		for _, field := range []string{"keyword", "category", "page"} {
			if value := strings.TrimSpace(c.Query(field)); value != "" {
				query.Set(field, value)
			}
		}
		if filterLang := strings.TrimSpace(c.Query("lang")); filterLang != "" {
			query.Set("filter_lang", filterLang)
		}
		editURLs[idx] = "/admin/i18n/edit?" + query.Encode()
	}
	data := gin.H{
		// title 是 i18n **key**（不是中文）：shell.Prepare → injectI18n 会按当前语言翻译它。
		// admin.i18n.title 是库内既有的词条（zh-CN「文案词条」/ 英文），所以这里复用它 ——
		// 写中文原文会让英文后台的页面标题恒为中文（其它页面标题都已经是 enums key）。
		"title":        "admin.i18n.title",
		"Entries":      items,
		"I18nEditURLs": editURLs,
		"Keyword":      filter.Keyword,
		"LangFilter":   filter.Lang,
		"CatFilter":    filter.Category,
		"Categories":   categories,
		// 写动作的结论不再回带（走 shell.RenderJump 渲染提示页，见 admin_jump.go）：
		// 模板里的 .Saved / .Errored / .Done / .Err 提示条整批删除。ListQuery 是表单 action
		// 上要带的列表筛选（渲染时拼、POST 回来由 shell.BackPath 读回）。
		"ListQuery": adminI18nPage.listQuery(c),
	}
	// 语言下拉的供数（字典只读口）：读失败给空列表 —— 页面仍可筛「全部语言」，
	// 只是少了按语言筛的能力，不该因此整页打不开。
	if h.dict != nil {
		if opts, derr := h.dict.ListDictOptions(c.Request.Context(), "language"); derr == nil {
			data["LangOptions"] = opts
		}
	}
	// 分页条：原版只渲染「第 X / Y 页」文字，没有页码链接 —— Total 超过一页时第 2 页起
	// 完全不可达（列表页最要紧的缺陷）。链接与筛选同源，翻页不丢 keyword / lang / category。
	base := shell.FilterBaseURL("/admin/i18n", map[string]string{
		"keyword":  filter.Keyword,
		"lang":     filter.Lang,
		"category": filter.Category,
	})
	for k, v := range shell.BuildPagination(total, page, adminI18nEntryPageSize, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/i18n", shell.Prepare(c, data))
}

// adminI18nSaveMissingMsg 保存词条时的缺项判定：返回**具体**缺哪一项的 enums 文案
// （按表单从上到下的顺序报第一项），都不缺时返回空串。
//
// 为什么不继续共用 MsgFieldRequired：那句话只说「有必填项没填」，而这张表单有三行 ——
// 运营得逐个试才知道是哪一行。三条文案的判据相同（都是客户端输入问题），差别只在
// 「说得够不够具体」。空值校验留在 handler 是因为 i18n.SaveEntry 把「空值」与
// 「存储不可用」都当同一条 error 返回，混在一起就没法在归口助手里区分了。
func adminI18nSaveMissingMsg(key, lang, value string) string {
	switch {
	case key == "":
		return adminenums.ErrI18nKeyEmpty
	case lang == "":
		return adminenums.ErrI18nLangEmpty
	case strings.TrimSpace(value) == "":
		return adminenums.ErrI18nValueEmpty
	}
	return ""
}

// adminI18nDeleteMissingMsg 删除词条时的缺项判定（只有 key 与语言两个输入，没有内容项）。
func adminI18nDeleteMissingMsg(key, lang string) string {
	switch {
	case key == "":
		return adminenums.ErrI18nKeyEmpty
	case lang == "":
		return adminenums.ErrI18nLangEmpty
	}
	return ""
}

// I18nEntryEditFragment reads an exact (key, lang) pair for the edit drawer.
func (h *adminI18nEntryHandle) I18nEntryEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	key, lang := strings.TrimSpace(c.Query("key")), strings.TrimSpace(c.Query("lang"))
	if key == "" || lang == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	entry, err := i18n.GetEntry(c.Request.Context(), key, lang)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(http.StatusNotFound)
		} else {
			adminErrParam(c, err)
			c.Status(http.StatusInternalServerError)
		}
		return
	}
	c.HTML(http.StatusOK, "admin/system/i18n_edit_form.html", shell.Prepare(c, gin.H{
		"I18nEdit": entry, "I18nListQuery": adminI18nEditFormQuery(c),
	}))
}

func (h *adminI18nEntryHandle) i18nEditFail(c *gin.Context, msg string) {
	if !adminDrawerHX(c) {
		// 原生提交（禁用 JS）：页面里没有就地放错误的位置，整页提示 + 回列表链接。
		adminI18nPage.fail(c, msg)
		return
	}
	key, lang := strings.TrimSpace(c.PostForm("key")), strings.TrimSpace(c.PostForm("lang"))
	entry, err := i18n.GetEntry(c.Request.Context(), key, lang)
	if err != nil || entry == nil {
		// 连词条都取不回来（已被删 / 存储不可用）：没有可回填的表单，整页提示。
		adminI18nPage.fail(c, msg)
		return
	}
	c.HTML(http.StatusOK, "admin/system/i18n_edit_form.html", shell.Prepare(c, gin.H{
		"I18nEdit": entry, "I18nListQuery": adminI18nPage.listQuery(c), "I18nEditErr": msg, "I18nEditEcho": map[string]string{
			"value": c.PostForm("value"), "category": c.PostForm("category"), "remark": c.PostForm("remark"),
		},
	}))
}

// I18nEntryUpdate only updates an existing pair, unlike SaveEntry's create/upsert path.
func (h *adminI18nEntryHandle) I18nEntryUpdate(c *gin.Context) {
	key, lang := strings.TrimSpace(c.PostForm("key")), strings.TrimSpace(c.PostForm("lang"))
	value := c.PostForm("value")
	if missing := adminI18nSaveMissingMsg(key, lang, value); missing != "" {
		h.i18nEditFail(c, response.TranslateMessage(c, missing))
		return
	}
	err := i18n.UpdateEntry(c.Request.Context(), i18n.Entry{
		Key: key, Lang: lang, Value: value, Category: c.PostForm("category"), Remark: c.PostForm("remark"),
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			h.i18nEditFail(c, response.TranslateMessage(c, adminenums.MsgBadRequest))
		} else {
			h.i18nEditFail(c, adminErrParam(c, err))
		}
		return
	}
	h.markI18nStale(c)
	// htmx 成功 → HX-Redirect 整页回列表；原生成功 → 提示页（1 秒后自动回列表）。
	adminI18nPage.jump(c, true, adminI18nSavedText(c, key, lang))
}

// I18nEntrySave POST /admin/i18n/save —— 新增或更新一条词条。
//
// 空值校验留在 handler（与同文件其它表单一致：administrators/roles/… 都先查必填再进 service）：
// i18n.SaveEntry 把「key/语言/内容为空」与「存储不可用 / 驱动报错」都当同一条 error 返回，
// 而下游的归口助手（adminErrParam）只放行 admin enums 白名单 —— 两类混在一起没法在那儿区分。
// 不在这里先拦，运营少填一个字段就会看到「操作失败」这种通用提示（任务 1 那种反向缺陷）。
// 拦掉之后，剩下能走到归口助手的只可能是基础设施错误。
func (h *adminI18nEntryHandle) I18nEntrySave(c *gin.Context) {
	key := strings.TrimSpace(c.PostForm("key"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	value := c.PostForm("value")
	if missing := adminI18nSaveMissingMsg(key, lang, value); missing != "" {
		adminI18nPage.fail(c, response.TranslateMessage(c, missing))
		return
	}
	entry := i18n.Entry{
		Key:      key,
		Lang:     lang,
		Value:    value,
		Category: c.PostForm("category"),
		Remark:   c.PostForm("remark"),
		Status:   1,
	}
	if err := i18n.SaveEntry(c.Request.Context(), entry); err != nil {
		// 只到这里才可能是基础设施错误：走页面路径归口（业务文案原样，原文进日志）。
		adminI18nPage.fail(c, adminErrParam(c, err))
		return
	}
	// 词条烘在产物字节里，改完必须标记站点待重建（否则站点停在旧文案且无报错）。
	h.markI18nStale(c)
	adminI18nPage.jump(c, true, adminI18nSavedText(c, key, lang))
}

// I18nEntryDelete POST /admin/i18n/delete —— 删除一条词条。
// 删除后构建期回退到组件包内的中文兜底（可见降级，不是空白）。
//
// 空值校验同 I18nEntrySave：DeleteEntry 的空 key/语言与存储不可用是两类错误，
// 前者必须让运营看见「哪个字段没填」，后者才走归口文案。
func (h *adminI18nEntryHandle) I18nEntryDelete(c *gin.Context) {
	key := strings.TrimSpace(c.PostForm("key"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	if missing := adminI18nDeleteMissingMsg(key, lang); missing != "" {
		adminI18nPage.fail(c, response.TranslateMessage(c, missing))
		return
	}
	if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
		adminI18nPage.fail(c, adminErrParam(c, err))
		return
	}
	// 删词条会让构建期回退到组件包内的中文兜底 —— 产物字节同样变了，必须标记待重建。
	h.markI18nStale(c)
	adminI18nPage.jump(c, true, adminI18nDeletedText(c, key, lang))
}

// adminI18nEntryKeySeparator 批量删除的复合键分隔符（"key|lang"）。
//
// 词条的唯一标识是 (key, lang) 二元组 —— 只带 key 会把其它语言下的同名词条
// 一起删掉（删 zh-CN 顺手删了 en-US）。分隔符选 "|"：key 是点分标识、
// lang 是 BCP-47（zh-CN），两者都不含它。万一真有人建了带 "|" 的 key，
// Cut 出来 lang 为空 → 计跳过，绝不会误删别的行（fail-safe，不是 fail-open）。
const adminI18nEntryKeySeparator = "|"

// I18nEntriesBulkDelete POST /admin/i18n/bulk-delete —— 批量删除词条。
//
// 逐条走同一条单条删除路径（同一个 i18n.DeleteEntry）：某一条失败（已不存在、
// key/lang 非法）只计入跳过数，整批不中断 —— 整批回滚会让用户以为「一条都没删」，
// 然后反复重试。结果按「已删除 N 条 / 跳过 M 条」回带，筛选与页码原样保留。
func (h *adminI18nEntryHandle) I18nEntriesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见「六领域公共：批量动作」一节）：文案走 shell 的受控出口，不直传原文。
		adminI18nPage.fail(c, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		key, lang, ok := strings.Cut(strings.TrimSpace(raw), adminI18nEntryKeySeparator)
		key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
		if !ok || key == "" || lang == "" {
			skipped++
			continue
		}
		if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	// 单条删除的失效在这里按批只发一次：逐条调会把"全站标记"重复 N 遍，
	// 而它标记的对象是同一批页面（结果等价，代价随批量线性放大）。
	if deleted > 0 {
		h.markI18nStale(c)
	}
	// 有跳过走失败档（提示更显眼，用户下次会去看剩下那些）；全成功走成功档。
	msg := adminI18nBulkDeleteResult(c, deleted, skipped)
	adminI18nPage.jump(c, skipped == 0, msg)
}

// adminI18nBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几条）。
func adminI18nBulkDeleteResult(c *gin.Context, deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return adminBulkTextOf(c, adminI18nBulkNoneSelected)
	case skipped == 0:
		return fmt.Sprintf(adminBulkTextOf(c, adminI18nBulkAllDeleted), strconv.Itoa(deleted))
	case deleted == 0:
		return fmt.Sprintf(adminBulkTextOf(c, adminI18nBulkAllSkipped), strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(adminBulkTextOf(c, adminI18nBulkPartial), strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
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

// adminSafeLangRedirect 校验回跳地址：只允许站内相对路径，其余一律回首页 "/"。
//
// 判据**只有一份**：shell.LangRedirectPath（渲染侧把值写进隐藏域时调的也是它）。
// 此前这里重抄了一遍判据（adminLangRedirectAllowed），靠 admin_notice_test.go 的对照用例
// 防漂移 —— 两份实现的漂移方向是静默的：放宽 = 多一个开放重定向面，收紧 = 运营切了语言
// 回不到原页面。现在判据归口到 shell，本函数只剩「拒绝时回首页」这一条消费侧语义。
//
// 为什么消费侧**必须**再校验一遍（而不是信任渲染侧写进隐藏域的值）：这个值从 query 带回来，
// 请求方可以直接改（admin/lang?redirect=//evil.example.com）。消费侧拿到的还是**已解码**
// 的值 —— 反斜杠、换行、NUL 都能出现，比渲染侧的 RequestURI 面更宽，而 LangRedirectPath
// 的判据本身就覆盖了这些（不含反斜杠 / 无控制字符），两边不需要各写一套。
// adminHomePath 控制面首页（仪表盘）—— 与 shell.adminHomePath 同值。
// / 已归前台首页（站点独占域名根），后台回落不能再指 "/"。
const adminHomePath = "/admin"

func adminSafeLangRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if p := shell.LangRedirectPath(raw); p != "" {
		return p
	}
	return adminHomePath
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

// MenusPage 菜单管理页（GET /admin/menus）。
//
// 列表是**树状分页**：Rows 已经按 DFS 前序摊平（顶级行 + 其子树），
// 层级用 Depth 表达、初始可见性用 Hidden/Expanded 表达（见 service.MenuPage）。
// 本页不再自己展平菜单树 —— 摊平与可见性都归 service，模板只渲染。
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
		"Err":           adminListLoadErr(c, err),
		"ListQuery":     adminMenusPage.listQuery(c),
		// SearchMode 让模板切到搜索态（给命中行标「匹配」、给祖先标「上级路径」）。
		"SearchMode": keyword != "",
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
	c.HTML(http.StatusOK, "admin/system/menu_edit_form.html", shell.Prepare(c, menuEditData(c, h, row, nil)))
}

// menuEditData 组装编辑抽屉的模板数据（正常打开与保存失败回显共用）。
//
// 候选取不到时**不传 Parents**，模板随之退化成 hidden parent_id：
// 编辑抽屉缺了上级下拉只是少一项能力，而缺了 parent_id 提交就会把菜单搬到顶级 ——
// 一个读失败不该换来一次静默的数据改动。
func menuEditData(c *gin.Context, h *AdminPagesHandle, row *admindto.MenuDetailResp,
	echo gin.H) gin.H {
	data := gin.H{"MenuEdit": row, "PermChoices": h.buildPermChoices(c, row.PermissionCodes)}
	if parents, err := h.menus.MenuParentOptions(c.Request.Context(), row.ID); err == nil {
		data["Parents"] = parents
	}
	for k, v := range echo {
		data[k] = v
	}
	return data
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
		// 原生提交（禁用 JS）：页面里没有就地放错误的位置，整页提示 + 回列表链接。
		adminMenusPage.fail(c, msg)
		return
	}
	id := shell.ParseUint(c.PostForm("id"))
	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: id})
	if id == 0 || err != nil || row == nil || row.ID != id {
		// 连菜单都取不回来：没有可回填的表单，整页提示。
		adminMenusPage.fail(c, msg)
		return
	}
	// 权限码是多值，塞不进上面那个 map[string]string；模板优先用它回显用户刚勾的那一屏，
	// 否则保存失败后勾选会退回库里的旧值，而用户正需要在这里改掉那个错误重试。
	// 候选项也要按**本次提交**重算勾选态，否则错误响应里的清单和回显的码对不上。
	// PermChoices 放在 echo 里覆盖 menuEditData 的默认值（后者按库里的码算），
	// 否则「保存失败 → 回显」会把用户刚勾的那一屏换回旧值。
	echo := gin.H{
		"MenuEditErr": msg,
		"MenuEditEcho": map[string]string{
			"title": c.PostForm("title"), "parent_id": c.PostForm("parent_id"),
			"type": c.PostForm("type"), "status": c.PostForm("status"),
			"path": c.PostForm("path"), "icon": c.PostForm("icon"),
			"sort_order": c.PostForm("sort_order"), "remark": c.PostForm("remark"),
		},
		"MenuEditEchoCodes": menuPermissionCodes(c),
		"PermChoices":       h.buildPermChoices(c, menuPermissionCodes(c)),
		// 上级下拉的选中值按 uint64 另给一份：MenuEditEcho 里是表单原始字符串，
		// 拿它跟候选 ID（uint64）比恒不相等，回显会让下拉跳回「（根菜单）」。
		"MenuEditParent": shell.ParseUint(c.PostForm("parent_id")),
	}
	c.HTML(http.StatusOK, "admin/system/menu_edit_form.html", shell.Prepare(c, menuEditData(c, h, row, echo)))
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
		adminMenusPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.menus.MenuCreate(c.Request.Context(), &admindto.MenuCreateReq{
		Title: title, ParentID: shell.ParseUint(c.PostForm("parent_id")),
		Type: shell.ParseStatus(c.PostForm("type")), Path: shell.FieldValue(c, "path"),
		Status: shell.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
		PermissionCodes: menuPermissionCodes(c),
	}); err != nil {
		adminMenusPage.writeFail(c, err)
		return
	}
	adminMenusPage.done(c)
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
		adminMenusPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	// 库里原值：表单只提交它**真正提供了编辑入口**的字段（标题 / 上级菜单 / 类型 / 状态 /
	// 路径 / 权限点 / 图标 / 排序 / 备注），其余列必须从原值带过来 —— MenuUpdate 是全量替换，
	// menuUpdateColumns 又把这几列显式写回，零值同样会落库。少了这一步，一次「只改上级菜单」
	// 的保存会悄悄清空外链地址 / 标题键 / 隐藏与公开标记，而界面上看不出任何异常。
	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: id})
	if err != nil || row == nil || row.ID != id {
		if adminDrawerHX(c) {
			h.menuEditFail(c, adminErrParam(c, errors.New(adminenums.ErrMenuNotFound)))
			return
		}
		adminMenusPage.writeFail(c, errors.New(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuUpdate(c.Request.Context(), &admindto.MenuUpdateReq{
		ID: id, Title: title, ParentID: shell.ParseUint(c.PostForm("parent_id")),
		Type: shell.ParseStatus(c.PostForm("type")), Path: shell.FieldValue(c, "path"),
		Status: shell.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
		PermissionCodes: menuPermissionCodes(c),
		// 图标有 hidden 输入（图标组件维护它的值），提交空串就是「去掉图标」，不做原值兜底。
		Icon: shell.FieldValue(c, "icon"),
		// 组件路径在这个界面没有编辑入口：类型未变时 service 整段跳过它（不校验、不写入）。
		TitleKey: row.TitleKey, ExternalURL: row.ExternalURL,
		IsHidden: row.IsHidden, IsPublic: row.IsPublic,
	}); err != nil {
		if adminDrawerHX(c) {
			h.menuEditFail(c, adminErrParam(c, err))
			return
		}
		adminMenusPage.writeFail(c, err)
		return
	}
	adminMenusPage.done(c)
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
		adminMenusPage.paramFail(c)
		return
	}
	nodes, err := h.menus.MenuTree(c.Request.Context(), &admindto.MenuTreeReq{})
	if err != nil {
		adminMenusPage.writeFail(c, err)
		return
	}
	if !adminMenuTreeHas(nodes, id) {
		adminMenusPage.writeFail(c, errors.New(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
		adminMenusPage.writeFail(c, err)
		return
	}
	adminMenusPage.done(c)
}

// MenusBulkDelete 批量删除菜单（POST /admin/menus/bulk-delete）。
//
// 系统菜单与有子菜单的节点由 service 拒绝、其余照常删除；单条失败只计数不中断整批。
func (h *AdminPagesHandle) MenusBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见「六领域公共：批量动作」一节）：文案走 shell 的受控出口，不直传原文。
		adminMenusPage.fail(c, shell.BulkIDsFacingText(c, berr))
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
	ok, msg := adminBulkOutcome(c, adminBulkNounMenu, deleted, skipped)
	adminMenusPage.jump(c, ok, msg)
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
		res = &admindto.PermListResp{}
	}
	data := shell.Prepare(c, gin.H{
		"title":        pagesMsgPermissionsTitle,
		"menu":         "permissions",
		"Rows":         res.List,
		"Total":        res.Total,
		"FilterCode":   code,
		"FilterModule": module,
		"Err":          adminListLoadErr(c, err),
		"ListQuery":    adminPermsPage.listQuery(c),
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
		// 原生提交（禁用 JS）：页面里没有就地放错误的位置，整页提示 + 回列表链接。
		adminPermsPage.fail(c, msg)
		return
	}
	id := shell.ParseUint(c.PostForm("id"))
	row, err := h.perms.PermDetail(c.Request.Context(), &admindto.PermDetailReq{ID: id})
	if id == 0 || err != nil || row == nil || row.ID != id {
		// 连权限点都取不回来：没有可回填的表单，整页提示。
		adminPermsPage.fail(c, msg)
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
		adminPermsPage.paramFail(c)
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
		adminPermsPage.writeFail(c, err)
		return
	}
	adminPermsPage.done(c)
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
		adminPermsPage.paramFail(c)
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
		adminPermsPage.writeFail(c, err)
		return
	}
	adminPermsPage.done(c)
}

// PermissionsDelete 删除权限点（POST /admin/permissions/delete）。
func (h *AdminPagesHandle) PermissionsDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPermsPage.paramFail(c)
		return
	}
	if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
		adminPermsPage.writeFail(c, err)
		return
	}
	adminPermsPage.done(c)
}

// PermissionsBulkDelete 批量删除权限点（POST /admin/permissions/bulk-delete）。
//
// 已分配给角色或被菜单引用的权限点由 service 拒绝、其余照常删除；
// 单条失败只计数不中断整批。
func (h *AdminPagesHandle) PermissionsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见「六领域公共：批量动作」一节）：文案走 shell 的受控出口，不直传原文。
		adminPermsPage.fail(c, shell.BulkIDsFacingText(c, berr))
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
	ok, msg := adminBulkOutcome(c, adminBulkNounPermission, deleted, skipped)
	adminPermsPage.jump(c, ok, msg)
}

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
		"Err":           adminListLoadErr(c, err),
		"ListQuery":     adminRolesPage.listQuery(c),
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
//	· 原生成功 / 失败 → 整页提示（shell.RenderJump，见 admin_jump.go）—— 成功 1 秒后自动
//	  回角色列表，失败不自动跳。
//
// 不带成功回执进 URL：抽屉里那句回执走模板自身的成功态（admin.roles.perm.saved），
// 列表页那条成功提示由提示页在响应体里给出。
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
		// 缺 role_id：没有可回的抽屉（片段由 role_id 决定），整页提示回角色列表并说明原因。
		adminRolesPage.paramFail(c)
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
			// 原生提交（禁用 JS）：页面里没有就地放错误的位置，整页提示回列表。
			// htmx 但树也取不回来时走同一出口 —— 此时只剩空树可渲，片段会退化成
			// 「没有可分配的菜单」的空态，那比一句通用错误更误导（像权限被清空了）。
			adminRolesPage.fail(c, msg)
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
	adminRolesPage.done(c)
}

// RolesCreate 新建角色（POST /admin/roles/create）。
func (h *AdminPagesHandle) RolesCreate(c *gin.Context) {
	roleCode := shell.FieldValue(c, "role_code")
	roleName := shell.FieldValue(c, "role_name")
	if roleCode == "" || roleName == "" {
		adminRolesPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.roles.RoleCreate(c.Request.Context(), &admindto.RoleCreateReq{
		RoleCode: roleCode, RoleName: roleName, Status: shell.ParseStatusPtr(c.PostForm("status")),
		SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminRolesPage.writeFail(c, err)
		return
	}
	adminRolesPage.done(c)
}

// RolesUpdate 编辑角色（POST /admin/roles/update）。
func (h *AdminPagesHandle) RolesUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	roleName := shell.FieldValue(c, "role_name")
	if id == 0 || roleName == "" {
		adminRolesPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(shell.FieldValue(c, "sort_order"))
	if err := h.roles.RoleUpdate(c.Request.Context(), &admindto.RoleUpdateReq{
		ID: id, RoleName: roleName, Status: shell.ParseStatus(c.PostForm("status")),
		SortOrder: sortOrder, Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminRolesPage.writeFail(c, err)
		return
	}
	adminRolesPage.done(c)
}

// RolesDelete 删除角色（POST /admin/roles/delete）。
func (h *AdminPagesHandle) RolesDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminRolesPage.paramFail(c)
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
		adminRolesPage.writeFail(c, err)
		return
	}
	adminRolesPage.done(c)
}

// RolesBulkDelete 批量删除角色（POST /admin/roles/bulk-delete）。
//
// 系统内置角色由 service 拒绝、其余照常删除；单条失败只计数不中断整批。
func (h *AdminPagesHandle) RolesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见「六领域公共：批量动作」一节）：文案走 shell 的受控出口，不直传原文。
		adminRolesPage.fail(c, shell.BulkIDsFacingText(c, berr))
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
	ok, msg := adminBulkOutcome(c, adminBulkNounRole, deleted, skipped)
	adminRolesPage.jump(c, ok, msg)
}

const (
	// dataruleEditorMarker 编辑器隐藏标记字段名。
	dataruleEditorMarker = "config_editor"
	// dataruleMaxGroups / dataruleMaxRowsPerGroup 一组上限：规则是给人读的，
	// 条件铺到几十条时更该拆成多条规则（组间已经是 AND，拆开语义不变）。
	dataruleMaxGroups       = 8
	dataruleMaxRowsPerGroup = 12
)

// dataruleOption 一个下拉选项（值 + 显示文本 + 是否选中）。
type dataruleOption struct {
	Value    string
	Label    string
	Selected bool
}

// dataruleFieldView 屏蔽字段多选里的一项。
type dataruleFieldView struct {
	Field   string
	Label   string
	Omitted bool
}

// dataruleRowView 一个条件行。
type dataruleRowView struct {
	FieldOptions []dataruleOption
	OpOptions    []dataruleOption
	Value        string
}

// dataruleGroupView 一个条件组。
type dataruleGroupView struct {
	Index     int
	Logic     string
	Rows      []dataruleRowView
	CanAddRow bool
}

// dataruleEditorCtx 配置编辑器片段的数据。
type dataruleEditorCtx struct {
	Domain      string
	FieldCount  int
	Fields      []dataruleFieldView
	Groups      []dataruleGroupView
	CanAddGroup bool
}

// dataruleFormRowRe 解析 groups[g].conditions[i].{field,op,value}。
var dataruleFormRowRe = regexp.MustCompile(`^groups\[(\d+)\]\.conditions\[(\d+)\]\.(field|op|value)$`)

// dataruleFormLogicRe 解析 groups[g].logic。
var dataruleFormLogicRe = regexp.MustCompile(`^groups\[(\d+)\]\.logic$`)

// dataruleConfigFromForm 从表单重建规则配置（保留空组与空行，供编辑器回渲染）。
func dataruleConfigFromForm(c *gin.Context) admindto.RuleConfigDTO {
	// 自己保证表单已解析：本函数直接读 Request.PostForm，若指望调用方先摸过一次
	// c.PostForm，换个调用顺序（或换个入口）就会静默解析出空配置 —— 用户填的条件
	// 会全部消失，且没有任何报错。
	_ = c.Request.ParseForm()

	cfg := admindto.RuleConfigDTO{OmitFields: c.PostFormArray("omit_fields")}
	if cfg.OmitFields == nil {
		cfg.OmitFields = []string{}
	}

	// 组序与行序都由表单单键决定：先扫出所有出现过的 (g, i) 与 g.logic，
	// 再按升序填充，保证「删掉中间一行」之后剩下的行仍然按屏幕上的顺序落库。
	type rowKey struct{ group, row int }
	rows := map[rowKey]*admindto.RuleConditionDTO{}
	logics := map[int]string{}
	maxGroup, maxRow := -1, map[int]int{}
	for key, values := range c.Request.PostForm {
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		if m := dataruleFormRowRe.FindStringSubmatch(key); m != nil {
			g, _ := strconv.Atoi(m[1])
			i, _ := strconv.Atoi(m[2])
			item, ok := rows[rowKey{g, i}]
			if !ok {
				item = &admindto.RuleConditionDTO{}
				rows[rowKey{g, i}] = item
			}
			switch m[3] {
			case "field":
				item.Field = strings.TrimSpace(value)
			case "op":
				item.Op = strings.TrimSpace(value)
			case "value":
				item.Value = strings.TrimSpace(value)
			}
			if g > maxGroup {
				maxGroup = g
			}
			if i+1 > maxRow[g] {
				maxRow[g] = i + 1
			}
			continue
		}
		if m := dataruleFormLogicRe.FindStringSubmatch(key); m != nil {
			g, _ := strconv.Atoi(m[1])
			logics[g] = strings.TrimSpace(value)
			if g > maxGroup {
				maxGroup = g
			}
		}
	}

	groups := make([]admindto.RuleConditionGroupDTO, 0, maxGroup+1)
	for g := 0; g <= maxGroup; g++ {
		group := admindto.RuleConditionGroupDTO{Logic: logics[g]}
		for i := 0; i < maxRow[g]; i++ {
			if item, ok := rows[rowKey{g, i}]; ok {
				group.Conditions = append(group.Conditions, *item)
			}
		}
		groups = append(groups, group)
	}
	cfg.ConditionGroups = groups
	return cfg
}

// dataruleUsableConditions 过滤掉「只有空壳」的条件行。
//
// 判据是**值也必须有**：引擎对空值会生成 field = ”（一条永远匹配不到的条件），
// 而空字段名会被直接丢弃 —— 两种都会让「看起来配了三条、实际只生效一条」。
// 落库前统一清掉，页面上的空行则原样保留给人继续填。
func dataruleUsableConditions(rows []admindto.RuleConditionDTO) []admindto.RuleConditionDTO {
	kept := make([]admindto.RuleConditionDTO, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Field) == "" || strings.TrimSpace(row.Value) == "" {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

// dataruleDropEmptyGroups 丢弃没有任何可用条件的组。
func dataruleDropEmptyGroups(cfg admindto.RuleConfigDTO) admindto.RuleConfigDTO {
	groups := make([]admindto.RuleConditionGroupDTO, 0, len(cfg.ConditionGroups))
	for _, group := range cfg.ConditionGroups {
		if conditions := dataruleUsableConditions(group.Conditions); len(conditions) > 0 {
			group.Conditions = conditions
			groups = append(groups, group)
		}
	}
	cfg.ConditionGroups = groups
	return cfg
}

// dataruleEditorContext 组装编辑器片段数据：该域的白名单 + 当前配置的组/行视图。
func (h *AdminPagesHandle) dataruleEditorContext(c *gin.Context, domain string, cfg admindto.RuleConfigDTO) dataruleEditorCtx {
	// 取词函数：下拉里的空选项与提示由本文件拼进片段 HTML（模板层不参与这些句子），
	// 所以在这里取一次当前语言（迁移 452 seed 中英词条）。
	tr := shell.TranslateFor(c)
	ctx := dataruleEditorCtx{
		Domain:     domain,
		Fields:     []dataruleFieldView{},
		Groups:     []dataruleGroupView{},
		FieldCount: 0,
	}

	detail, _ := h.rules.RuleSchemaDetail(c.Request.Context(), &admindto.RuleSchemaDetailReq{Domain: domain})
	labels := map[string]string{}
	opsByField := map[string][]string{}
	order := make([]string, 0, 8)
	if detail != nil {
		for _, f := range detail.Fields {
			order = append(order, f.Field)
			labels[f.Field] = f.Label
			opsByField[f.Field] = f.Operators
		}
	}
	ctx.FieldCount = len(order)

	omitted := map[string]bool{}
	for _, f := range cfg.OmitFields {
		omitted[f] = true
	}
	for _, field := range order {
		ctx.Fields = append(ctx.Fields, dataruleFieldView{Field: field, Label: labels[field], Omitted: omitted[field]})
	}

	fieldOptions := func(selected string) []dataruleOption {
		options := make([]dataruleOption, 0, len(order)+1)
		options = append(options, dataruleOption{Value: "", Label: tr(adminenums.DatarulesEditorFieldPlaceholder, "请选择字段"), Selected: selected == ""})
		for _, field := range order {
			options = append(options, dataruleOption{
				Value:    field,
				Label:    labels[field] + "（" + field + "）",
				Selected: field == selected,
			})
		}
		return options
	}
	opOptions := func(field, selected string) []dataruleOption {
		ops := opsByField[field]
		if len(ops) == 0 {
			return []dataruleOption{{Value: "", Label: tr(adminenums.DatarulesEditorFieldRequired, "请先选择字段"), Selected: true}}
		}
		options := make([]dataruleOption, 0, len(ops))
		for _, op := range ops {
			options = append(options, dataruleOption{Value: op, Label: op, Selected: op == selected})
		}
		return options
	}

	groups := cfg.ConditionGroups
	if len(groups) > dataruleMaxGroups {
		groups = groups[:dataruleMaxGroups]
	}
	for gi, group := range groups {
		logic := datarule.LogicAnd
		if strings.EqualFold(strings.TrimSpace(group.Logic), datarule.LogicOr) {
			logic = datarule.LogicOr
		}
		view := dataruleGroupView{Index: gi, Logic: logic, CanAddRow: len(group.Conditions) < dataruleMaxRowsPerGroup}
		for _, row := range group.Conditions {
			view.Rows = append(view.Rows, dataruleRowView{
				FieldOptions: fieldOptions(row.Field),
				OpOptions:    opOptions(row.Field, row.Op),
				Value:        row.Value,
			})
		}
		ctx.Groups = append(ctx.Groups, view)
	}
	if len(ctx.Groups) == 0 {
		// 至少给一组一行可编辑（空列表会让「添加条件」无处落脚）。
		ctx.Groups = append(ctx.Groups, dataruleGroupView{
			Index:     0,
			Logic:     datarule.LogicAnd,
			Rows:      []dataruleRowView{{FieldOptions: fieldOptions(""), OpOptions: opOptions("", "")}},
			CanAddRow: true,
		})
	}
	ctx.CanAddGroup = len(ctx.Groups) < dataruleMaxGroups
	return ctx
}

// DataruleConfigEditor 配置编辑器片段（HTMX）：add_row / del_row / add_group / del_group / refresh。
//
// 它不落库、也不做业务校验 —— 只是把「当前表单里已有的配置」按该域白名单重新渲染一遍，
// 所以不挂 Casbin（与 product 的属性值行编辑器一致；真正的写入仍走 /admin/datarules/update）。
func (h *AdminPagesHandle) DataruleConfigEditor(c *gin.Context) {
	domain := shell.FieldValue(c, "domain")
	cfg := dataruleConfigFromForm(c)

	switch shell.FieldValue(c, "action") {
	case "add_group":
		if len(cfg.ConditionGroups) < dataruleMaxGroups {
			cfg.ConditionGroups = append(cfg.ConditionGroups, admindto.RuleConditionGroupDTO{Logic: datarule.LogicAnd})
		}
	case "del_group":
		gi := int(shell.ParseUint(c.PostForm("group")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) {
			cfg.ConditionGroups = append(cfg.ConditionGroups[:gi], cfg.ConditionGroups[gi+1:]...)
		}
	case "add_row":
		gi := int(shell.ParseUint(c.PostForm("group")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) && len(cfg.ConditionGroups[gi].Conditions) < dataruleMaxRowsPerGroup {
			cfg.ConditionGroups[gi].Conditions = append(cfg.ConditionGroups[gi].Conditions, admindto.RuleConditionDTO{})
		}
	case "del_row":
		gi := int(shell.ParseUint(c.PostForm("group")))
		ri := int(shell.ParseUint(c.PostForm("row")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) && ri >= 0 && ri < len(cfg.ConditionGroups[gi].Conditions) {
			rows := cfg.ConditionGroups[gi].Conditions
			cfg.ConditionGroups[gi].Conditions = append(rows[:ri], rows[ri+1:]...)
		}
	}

	c.HTML(http.StatusOK, "admin/system/datarule_config_editor.html",
		shell.Prepare(c, gin.H{"Editor": h.dataruleEditorContext(c, domain, cfg)}))
}

// bootstrapDataRule 装配 admin 的数据权限：注册数据域 → 注入 RuleProvider → 挂 GORM 插件。
//
// 域声明（白名单、表名）来自 AdminEntity 字段上的 datarule tag，本函数只负责把它交给引擎；
// 声明写错（未知操作符、缺 label、表名对不上）在这里直接 panic：域没注册上的后果是
// resolveDomain 永不命中，该表的行级过滤整体静默失效（beforeQuery fail-open）——
// 这类错必须停在启动阶段，不能等到「规则存进去了却一条都没拦住」才发现。
func bootstrapDataRule(svc *adminservice.Service, db *gorm.DB) {
	domain, err := adminmodel.AdminDataRuleDomain()
	if err != nil {
		panic("admin 数据域声明不合法: " + err.Error())
	}
	if err := datarule.RegisterDomain(domain); err != nil {
		panic("admin 数据域注册失败: " + err.Error())
	}

	datarule.SetProvider(svc)
	if err := datarule.RegisterPluginWithDB(db); err != nil {
		logger.Scene("init").Error(err, "注册 datarule GORM 插件失败")
	}
}

// DevLogin 开发阶段免密登录：建立超管会话后 302 到目标页面。
//
// 只应在 debug 模式下注册。参数：
//   - to     登录后跳转的路径（以 / 开头；缺省 /admin）
//   - user   可选，指定超管用户名（缺省取第一个超管）
func (h *Handle) DevLogin(c *gin.Context) {
	// 锁 2：只认环回。RemoteAddr 形如 127.0.0.1:52331 / [::1]:52331。
	if !isLoopbackRemote(c.Request.RemoteAddr) {
		logger.Scene("admin").With("remote", c.Request.RemoteAddr).Warn("拒绝非本机的一键登录请求")
		// 文案走 key + 中文兜底：这几句是 c.String 直出的响应体，不经过模板取词层，
		// 写死中文会让英文后台的运营看到中文错误（迁移 452 seed 中英词条）。
		c.String(http.StatusForbidden, shell.TranslateFor(c)("admin.dev_login.loopback_only", "开发登录仅限本机访问"))
		return
	}

	res, err := h.admin.DevLogin(c.Request.Context(), strings.TrimSpace(c.Query("user")))
	if err != nil {
		logger.Scene("admin").Error(err, "开发登录失败")
		c.String(http.StatusInternalServerError, shell.TranslateFor(c)("admin.dev_login.failed", "开发登录失败，详情见服务端日志"))
		return
	}

	// 锁 4：与正常登录同一条会话路径（cookie + Redis + CSRF 轮换）。
	if err := auth.SaveCookieSession(c, &auth.CookieSession{
		SessionID: res.SessionID,
	}, false); err != nil {
		logger.Scene("admin").Error(err, "开发登录写会话失败")
		c.String(http.StatusInternalServerError, shell.TranslateFor(c)("admin.dev_login.session_failed", "写会话失败，详情见服务端日志"))
		return
	}
	if _, err := builtin.RotateCSRFToken(c); err != nil {
		logger.Scene("admin").Error(err, "开发登录轮换 CSRF token 失败")
		c.String(http.StatusInternalServerError, shell.TranslateFor(c)("admin.dev_login.csrf_failed", "轮换 CSRF token 失败，详情见服务端日志"))
		return
	}

	to := safeRedirect(c.Query("to"))
	logger.Scene("admin").With("username", res.Username).With("to", to).
		Info("开发阶段一键登录（仅 debug 模式可用）")
	c.Redirect(http.StatusFound, to)
}

// DevLoginHandler 供装配层在 debug 模式下挂载（见 internal/routers/routes.go）。
//
// 独立工厂而不是复用 SetupAdminRoutes 里的 handle：那个 handle 服务的是 /api/admin/* 组，
// 而一键登录是**页面**路由（浏览器直接访问 /admin/dev-login）。
func DevLoginHandler(db *gorm.DB) gin.HandlerFunc {
	return NewHandle(adminservice.NewService(db)).DevLogin
}

// isLoopbackRemote 判断远端地址是否为本机（用于把开发登录限制在本机）。
func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// safeRedirect 只接受站内绝对路径，其余一律回 /admin。
//
// 开放重定向在「一键登录」这种带凭据的入口上尤其危险：
// 一个 https://evil.com 的 to 参数能让人以为自己在登录本站。
func safeRedirect(to string) string {
	to = strings.TrimSpace(to)
	if to == "" || !strings.HasPrefix(to, "/") {
		return "/admin"
	}
	// "//evil.com" 是协议相对 URL，浏览器会当外站处理，必须挡住。
	if strings.HasPrefix(to, "//") || strings.HasPrefix(to, "/\\") {
		return "/admin"
	}
	return to
}
