package adminhttp

// admin_pages_handle.go — 管理页处理器的主文件：结构体与构造、六领域公共批量出口、页面写操作失败出口，
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

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/web/shell"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"

	admincontract "go_wp/internal/module/admin/contract"
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

func adminDrawerRedirect(c *gin.Context, target string) {
	if adminDrawerHX(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusSeeOther, target)
}
