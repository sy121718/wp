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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/config"
	"go_wp/internal/web/shell"
	"go_wp/pkg/captcha"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
)

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
// adminBulkResultURL 批量动作结果回带：有跳过走 ?err=（含成功条数），全成功走 ?done=。
// 模板与名词都经 adminBulkTextOf 按当前语言取（与读侧 adminDoneTexts / adminErrTexts 同一个取法）：
// 文案里的数字一律先 strconv.Itoa 成字符串再填（词条只允许 %s，理由见 pkg/i18n.HasStringPlaceholdersOnly）。
func adminBulkResultURL(c *gin.Context, path string, noun adminBulkText, deleted, skipped int) string {
	nounText := adminBulkTextOf(c, noun)
	switch {
	case skipped > 0:
		tpl := adminBulkTextOf(c, adminBulkPartialText)
		return path + "?err=" + url.QueryEscape(fmt.Sprintf(tpl, strconv.Itoa(deleted), nounText, strconv.Itoa(skipped)))
	case deleted > 0:
		tpl := adminBulkTextOf(c, adminBulkDoneText)
		return path + "?done=" + url.QueryEscape(fmt.Sprintf(tpl, strconv.Itoa(deleted), nounText))
	}
	// 既没删也没跳过 = 请求里没有一个可用的 id（没勾选 / 勾的全是空值）。
	// 此前这里返回**裸路径**：用户回到列表页，页面上什么都没发生，与「删了但列表没刷新」
	// 无法区分，只能反复点。走 ?err= 明确说明「这次提交没带任何可操作项」。
	return path + "?err=" + url.QueryEscape(response.TranslateMessage(c, adminenums.MsgBadRequest))
}

// --- 页面写操作的失败出口 ---
//
// 每一个写 handler 的失败都必须回到**页面**：303（StatusSeeOther）回列表页 / 来源页，
// 原因经 ?err= 回带，由读侧 adminPageErrText（形状清洗 + 受控文案白名单）决定是否渲染。
//
// 修之前这里是两个脱离页面的出口，用户提交失败后只能按浏览器后退：
//   · 参数级失败 —— c.String(400, pagesMsgFieldRequired)：响应体是 i18n 的 key 本身
//     （页面根本没机会出现这句话），浏览器停在 POST 路径上；
//   · service 失败 —— adminWriteFailed：它写的是 JSON（{"code":400,"message":"角色不存在"}），
//     而这里的请求来自表单提交，JSON 对用户没有任何意义。
//
// 为什么是 303 而不是 302：POST 之后必须换成 GET 才回页面（否则刷新会重发表单），
// 与同文件其余成功路径（c.Redirect(http.StatusSeeOther, …)）同一取舍。
//
// 文案只有两条来源，且都必须在读侧候选里（不能自己造第二份）：
//   · 参数级 → MsgBadRequest 的当前语言译文（在 AdminFacingMessages 白名单里）；
//   · service 错误 / 业务判定 → adminErrParam（同一份白名单 + 同一处结构化日志：业务文案
//     原样透出，未命中落 ErrInternal 归口文案）。

// adminPageParamFail 参数级校验失败的页面出口（缺 id / 缺必填字段）。
func adminPageParamFail(c *gin.Context, back string) {
	c.Redirect(http.StatusSeeOther, adminPageErrURL(back, response.TranslateMessage(c, adminenums.MsgBadRequest)))
}

// adminPageWriteFail service 写失败（含「查不到」这类业务判定）的页面出口。
func adminPageWriteFail(c *gin.Context, back string, err error) {
	c.Redirect(http.StatusSeeOther, adminPageErrURL(back, adminErrParam(c, err)))
}

// adminPageErrURL 回跳地址的构造点：在目标路径上追加 err=<受控文案>。
//
// 目标可能已经带查询串（权限点页保留 code/module 筛选、数据规则编辑页带 id），
// 所以分隔符按目标自身决定 —— 拼成 `…?a=b?err=…` 会让读侧取不到 err，
// 表现是「提交失败但页面上没有任何提示」，比不改更糟。
func adminPageErrURL(back, errText string) string {
	sep := "?"
	if strings.Contains(back, "?") {
		sep = "&"
	}
	return back + sep + "err=" + url.QueryEscape(errText)
}

// adminPermissionsBackURL 权限点页的回跳目标：保留列表页的 code/module 筛选。
//
// 筛选是服务端条件（进 SQL），丢了它用户会从「筛到的那几条」跳到全量列表，
// 看不出这次失败对应的是哪一条。取 query 而非表单：筛选是列表页 URL 上的参数。
func adminPermissionsBackURL(c *gin.Context) string {
	return shell.FilterBaseURL("/admin/permissions", map[string]string{
		"code":   strings.TrimSpace(c.Query("code")),
		"module": strings.TrimSpace(c.Query("module")),
	})
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

func adminDrawerHX(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

func adminDrawerRedirect(c *gin.Context, target string) {
	if adminDrawerHX(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusSeeOther, target)
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
		"Err":          adminErrOrLoad(c, err),
		"Done":         adminPageDone(c, c.Query("done")),
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
		adminPageParamFail(c, "/admin/datarules")
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: admindto.RuleConfigDTO{},
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/datarules", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesEditPage 数据规则编辑页（GET /admin/datarules/edit?id=X）。
// 从 RuleDetail 回显 rule_name/domain/status/remark 与 config JSON。
func (h *AdminPagesHandle) DatarulesEditPage(c *gin.Context) {
	id := shell.ParseUint(c.Query("id"))
	if id == 0 {
		// 页面请求的失败出口是**页面**：303 回列表页并把原因经 ?err= 回带（读侧
		// adminPageErrText 白名单放行）。原先是 c.String(400, pagesMsgFieldRequired) ——
		// 响应体是 i18n 的 key 本身（用户看到内部标识符），而且脱离页壳。
		// 走与其它写 handler 同一个参数级出口（文案取 MsgBadRequest 的译文，在白名单里）。
		adminPageParamFail(c, "/admin/datarules")
		return
	}
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil || detail == nil {
		// id 存在但查不到（已被别人删掉 / 不在本工程作用域）：与上面「缺 id」同一形状的失败 ——
		// 也不能直出裸文本。原先是 c.String(404, "数据规则不存在")：响应体是那句中文、
		// **没有页壳**，用户在编辑页上点了半天链接后落到一个纯文本页面。
		// 文案取 adminenums.ErrRuleNotFound（在 AdminFacingMessages 白名单里，读侧候选天然覆盖）。
		adminPageWriteFail(c, "/admin/datarules", errors.New(adminenums.ErrRuleNotFound))
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	// 条件编辑器按该数据域的白名单渲染（字段 / 操作符下拉都来自域声明）。
	editor := h.dataruleEditorContext(c, detail.Domain, detail.Config)
	c.HTML(http.StatusOK, "admin/system/datarule_edit", shell.Prepare(c, gin.H{
		"title":   pagesMsgDatarulesTitle,
		"menu":    "datarules",
		"Detail":  detail,
		"Domains": domains,
		"Editor":  editor,
		// 错误槽位：保存失败会 303 回本页并带 ?err=（见 DatarulesUpdate 的分流），
		// 这一页必须能把它渲染出来 —— 否则用户看到的是「点了保存、页面刷新了一下、
		// 什么都没发生」，比回到列表页更难判断。
		"Err": adminPageErrText(c, c.Query("err")),
	}))
}

// adminDataruleBackURL /admin/datarules/update 的失败回跳目标：按提交**来源**分流。
//
// 这是本域唯一「一个端点两个入口」的写操作 —— 列表页的抽屉表单（datarule.html，只改
// rule_name/domain/status/remark）与编辑页的完整表单（datarule_edit.html，还带条件配置）
// 都 POST 到 /admin/datarules/update。分流依据**本来就有**：编辑页提交时带
// dataruleEditorMarker（config_editor 隐藏域，见 datarule_config_form.go 的表单约定），
// 抽屉不带 —— 用它判来源，不新增协议、不猜别的字段。
//
// 缺 id（0）时没有可回的编辑页（它由 id 决定），一律回列表页。
func adminDataruleBackURL(c *gin.Context, id uint64) string {
	if id == 0 || c.PostForm(dataruleEditorMarker) == "" {
		return "/admin/datarules"
	}
	return fmt.Sprintf("/admin/datarules/edit?id=%d", id)
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
	// 失败出口回**来源页**（判定依据见 adminDataruleBackURL）：编辑页提交就回编辑页，
	// 抽屉提交就回列表页 —— 编辑页那份表单填一次成本很高，甩回列表页等于让他重填。
	back := adminDataruleBackURL(c, id)
	if id == 0 || ruleName == "" || domain == "" {
		adminPageParamFail(c, back)
		return
	}
	config := admindto.RuleConfigDTO{}
	if c.PostForm(dataruleEditorMarker) != "" {
		config = dataruleDropEmptyGroups(dataruleConfigFromForm(c))
	} else {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil {
			adminPageWriteFail(c, back, detailErr)
			return
		}
		if detail == nil {
			// 「查不到但也没报错」必须给一条可行动的业务文案（规则不存在 / 不在本工程作用域内），
			// 不能像修复前那样拿 nil 去调错误出口（旧出口对 nil 直接 return → 200 空体，
			// 前端看到「点了没反应」而日志里什么都没有）。
			adminPageWriteFail(c, back, errors.New(adminenums.ErrRuleNotFound))
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, back, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesDelete 删除数据规则（POST /admin/datarules/delete）。
func (h *AdminPagesHandle) DatarulesDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, "/admin/datarules")
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		adminPageWriteFail(c, "/admin/datarules", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesBulkDelete 批量删除数据规则（POST /admin/datarules/bulk-delete）。
//
// 逐条走同一条单条删除路径（含该规则的分配记录清理）；单条失败只计数不中断整批。
func (h *AdminPagesHandle) DatarulesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusSeeOther, "/admin/datarules?err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
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
	c.Redirect(http.StatusSeeOther, adminBulkResultURL(c, "/admin/datarules", adminBulkNounDatarule, deleted, skipped))
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
}

// adminI18nPageMarker 页面侧的最小失效端口（消费者侧定义，跨模块只依赖这一条）。
type adminI18nPageMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

// SetPageMarker 注入页面失效端口（装配期）。
func (h *adminI18nEntryHandle) SetPageMarker(m adminI18nPageMarker) { h.pages = m }

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
		"Saved":        adminPageSaved(c.Query("saved")),
		"Errored":      adminPageErrText(c, c.Query("errored")),
		// 批量删除的结果条（?done= / ?err=）：与全站列表页同一对键，文案由服务端拼装
		// （受控文本 + 计数）。读侧一律过受控出口 —— ?done= 走 adminPageDone（与写侧共用
		// 模板字面量、整体匹配），?err= / ?errored= 走 adminPageErrText —— 因为**页面不是
		// 可信边界**：手拼一个 ?done=任意文案 就能伪造一条顶着「成功」样式的消息。
		"Done": adminPageDone(c, c.Query("done")),
		"Err":  adminPageErrText(c, c.Query("err")),
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
		"I18nEdit": entry, "I18nEditBack": adminI18nEditBack(c),
	}))
}

func (h *adminI18nEntryHandle) i18nEditFail(c *gin.Context, msg string) {
	if !adminDrawerHX(c) {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", msg))
		return
	}
	key, lang := strings.TrimSpace(c.PostForm("key")), strings.TrimSpace(c.PostForm("lang"))
	entry, err := i18n.GetEntry(c.Request.Context(), key, lang)
	if err != nil || entry == nil {
		adminDrawerRedirect(c, adminI18nBackURL(c, "errored", msg))
		return
	}
	c.HTML(http.StatusOK, "admin/system/i18n_edit_form.html", shell.Prepare(c, gin.H{
		"I18nEdit": entry, "I18nEditBack": adminI18nEditBack(c), "I18nEditErr": msg, "I18nEditEcho": map[string]string{
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
	adminDrawerRedirect(c, adminI18nBackURL(c, "saved", adminI18nEntryIdentity(key, lang)))
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
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored",
			response.TranslateMessage(c, missing)))
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
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", adminErrParam(c, err)))
		return
	}
	// 词条烘在产物字节里，改完必须标记站点待重建（否则站点停在旧文案且无报错）。
	h.markI18nStale(c)
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "saved", adminI18nEntryIdentity(key, lang)))
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
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored",
			response.TranslateMessage(c, missing)))
		return
	}
	if err := i18n.DeleteEntry(c.Request.Context(), key, lang); err != nil {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "errored", adminErrParam(c, err)))
		return
	}
	// 删词条会让构建期回退到组件包内的中文兜底 —— 产物字节同样变了，必须标记待重建。
	h.markI18nStale(c)
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "saved", adminI18nDeletedIdentity(key, lang)))
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
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "err", shell.BulkIDsFacingText(c, berr)))
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
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	msg := adminI18nBulkDeleteResult(c, deleted, skipped)
	if skipped > 0 {
		c.Redirect(http.StatusFound, adminI18nBackURL(c, "err", msg))
		return
	}
	c.Redirect(http.StatusFound, adminI18nBackURL(c, "done", msg))
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

// adminI18nEditBack carries the list filters from the edit GET into the update POST.
func adminI18nEditBack(c *gin.Context) map[string]string {
	back := map[string]string{
		"_keyword": c.PostForm("_keyword"), "_lang": c.PostForm("_lang"),
		"_category": c.PostForm("_category"), "_page": c.PostForm("_page"),
	}
	if c.Request.Method == http.MethodGet {
		back["_keyword"] = c.Query("keyword")
		back["_lang"] = c.Query("filter_lang")
		back["_category"] = c.Query("category")
		back["_page"] = c.Query("page")
	}
	return back
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
