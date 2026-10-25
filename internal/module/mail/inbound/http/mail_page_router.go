package mailhttp

// mail_page_router.go — 邮箱后台六页（/admin/mail*）的注册落点与鉴权对象声明。
//
// 只做注册：`/admin` 组的中间件链（SessionAuth + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 mail_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 写操作额外按**对应 API 的路径**走 Casbin 权限点，权限点路径与 API 完全同源（一个字符都不改）。
// 页面 GET 的 Casbin 待补（见 docs/02-Z §4.3）。
//
// 函数保持**不导出**：装配层只调 SetupMailRoutes，页面注册由它在同一位置调用。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	mailcontract "go_wp/internal/module/mail/contract"
	"go_wp/internal/permission"
)

// setupMailPageRoutes 注册邮箱后台页（pages = /admin 页面组）。
//
// 六页分工（一页一职能）：发信账号 / 邮件模板 / 联系人 / 群发活动 / 自动化流程 / 运行记录。
// 拆页前是「配置页（账号 + 模板）」与「营销页（联系人 + 活动）」两页，
// 每页两个职能、两个列表，完成最常见任务要在同一页里上下找。
func setupMailPageRoutes(pages *gin.RouterGroup, svc mailcontract.MailService) {
	if pages == nil || svc == nil {
		return
	}
	mailPage := NewMailPageHandle(svc)
	declareMailPageObjects()

	// —— 发信账号 ——
	pages.GET("/mail", adminui.PageAuthz("/api/mail/account/list"), mailPage.MailPage)
	pages.POST("/mail/account/save", builtin.CasbinMiddlewareForPath("/api/mail/account/save"), mailPage.MailAccountSave)
	pages.POST("/mail/account/delete", builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountDelete)
	pages.POST("/mail/account/default", builtin.CasbinMiddlewareForPath("/api/mail/account/default"), mailPage.MailAccountDefault)
	pages.POST("/mail/account/test", builtin.CasbinMiddlewareForPath("/api/mail/account/test"), mailPage.MailAccountTest)
	// —— 邮件模板 ——
	pages.GET("/mail/templates", adminui.PageAuthz("/api/mail/template/list"), mailPage.MailTemplatesPage)
	pages.POST("/mail/template/save", builtin.CasbinMiddlewareForPath("/api/mail/template/save"), mailPage.MailTemplateSave)
	pages.POST("/mail/template/delete", builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplateDelete)
	// 批量动作（评审规则 admin-ui-logic §7：列表首列勾选 + 批量条）。
	// 权限点与对应单条动作**完全同源**（同一个 CasbinMiddlewareForPath、同一组 API 路径），
	// 不新增权限点、不写迁移 —— 单列一条策略就等于把「能删一个」的人挡在批量外。
	pages.POST("/mail/accounts/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountsBulkDelete)
	pages.POST("/mail/templates/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplatesBulkDelete)

	// —— 联系人 ——
	pages.GET("/mail/contacts", adminui.PageAuthz("/api/mail/contact/list"), mailPage.MailContactsPage)
	pages.POST("/mail/contact/import", builtin.CasbinMiddlewareForPath("/api/mail/contact/import"), mailPage.MailContactImport)
	pages.POST("/mail/contact/status", builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactStatus)
	pages.POST("/mail/contacts/bulk-status", builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactsBulkStatus)
	// CRUD 与标签：**已全部迁到 JSON 端点**（见 mail_contact_write.go，注册见 mail_router.go）。
	// 页面表单与批量条按钮带 data-write-api 直调，四条页面 POST 因此下线：
	// 新建 / 编辑（原 POST /mail/contact/save）、单条删除（原 POST /mail/contact/delete）、
	// 批量删除与批量打标签（原 POST /mail/contacts/bulk-delete、/bulk-tag）。
	// 权限点与 obj 逐字不变（沿用迁移 525 的 /api/mail/contact/{save,delete,tag}），
	// 不新增权限点、不写权限迁移。
	//
	// 仍留在页面 POST 的只有批量改状态（/mail/contacts/bulk-status）：它没有 JSON 端点
	// （服务端改状态要同步写抑制名单），而批量条上「一个表单并排多个动作」现在由
	// json_write.js 的**按钮级声明**（读 e.submitter）支持 —— 表单自己不声明端点，
	// 所以这个按钮不会被邻居的声明继承成 fetch。

	// —— 群发活动（列表 + 报表）——
	pages.GET("/mail/campaigns", adminui.PageAuthz("/api/mail/campaign/list"), mailPage.MailCampaignsPage)
	pages.POST("/mail/campaign/save", builtin.CasbinMiddlewareForPath("/api/mail/campaign/save"), mailPage.MailCampaignSave)
	pages.POST("/mail/campaign/start", builtin.CasbinMiddlewareForPath("/api/mail/campaign/start"), mailPage.MailCampaignStart)
	pages.POST("/mail/campaign/delete", builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignDelete)
	pages.POST("/mail/campaigns/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignsBulkDelete)
	// 活动报表（#38 P1）：打开 / 点击 / 退订与收件人明细。报表是只读，权限沿用活动列表。
	pages.GET("/mail/campaign", adminui.PageAuthz("/api/mail/campaign/list"), mailPage.MailCampaignPage)

	// 旧「邮件营销」页已拆成「联系人」与「群发活动」两页。这里保留 302 而不是让路径 404：
	// 它此前同时是 sys_menus 的菜单项和用户书签，直接消失会让人以为功能被删了。
	// 处理函数放在 mailPageHandle 上（不是就地闭包）：这样测试能挂同一条路由断言
	// Location，而不必装配整条三层鉴权链。
	pages.GET("/mail/marketing", mailPage.MailMarketingRedirect)

	// —— 自动化 ——
	pages.GET("/mail/automation", adminui.PageAuthz("/api/mail/automation/list"), mailPage.MailAutomationPage)
	// 运行记录（排障）：流程是配置、实例是现场，拆成两页各答一个问题。
	pages.GET("/mail/automation/runs", adminui.PageAuthz("/api/mail/automation/list"), mailPage.MailAutomationRunsPage)
	pages.GET("/mail/automation/run", adminui.PageAuthz("/api/mail/automation/list"), mailPage.MailAutomationRunDetail)
	// 画布：**流程的唯一编辑器**。带 ?id=N 是编辑，不带 id 是新建 ——
	// 表单编辑页（旧 /mail/automation/edit）已删除，不留重定向：留一条跳转就等于
	// 「两个入口」以另一种形式复活。
	pages.GET("/mail/automation/canvas", adminui.PageAuthz("/api/mail/automation/list"), mailPage.MailAutomationCanvas)
	pages.POST("/mail/automation/status", builtin.CasbinMiddlewareForPath("/api/mail/automation/status"), mailPage.MailAutomationStatus)
	pages.POST("/mail/automation/delete", builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationDelete)
	pages.POST("/mail/automations/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationsBulkDelete)
	pages.POST("/mail/automation/tick", builtin.CasbinMiddlewareForPath("/api/mail/automation/tick"), mailPage.MailAutomationTick)
}

// declareMailPageObjects 把本模块后台页面入口的鉴权对象登记进权限声明表。
//
// 为什么必须显式做这一步：本模块的页面写操作复用的是**对应 API 的路径**当 obj
// （见文件头），而权限声明表（permission.declared）只由「带 permission.X 参数的路由注册」
// 填充 —— 页面路由走的是 CasbinMiddlewareForPath，登记不了自己。
//
// 实测后果：AI 工具的权限判定走 permission.RoutesOf(perm)，而这些权限点一个路由都查不到，
// 于是 casbinAuthorizer fail closed，工具调用一律 forbidden —— 而页面本身完全正常，
// 排查时会往「AI 权限配错了」的方向找，真正的原因在这里。
//
// 重复声明同值安全（Declare 遇到同 key 同权限点直接 return）；与 API 侧已声明的
// /api/mail/contact/status 等重叠也走这条路径 —— 联系人写三条（save / delete / tag）如今也有
// 真实路由了，这里保留声明是为了让**本模块的声明清单自成一份、不依赖装配顺序**
// （mail_page_router_test.go 只调本函数，不装路由）。
func declareMailPageObjects() {
	permission.Declare("POST", "/api/mail/account/save", permission.MailAccountSave)
	permission.Declare("POST", "/api/mail/account/delete", permission.MailAccountDelete)
	permission.Declare("POST", "/api/mail/account/default", permission.MailAccountDefault)
	permission.Declare("POST", "/api/mail/account/test", permission.MailAccountTest)
	permission.Declare("POST", "/api/mail/template/save", permission.MailTemplateSave)
	permission.Declare("POST", "/api/mail/template/delete", permission.MailTemplateDelete)
	permission.Declare("POST", "/api/mail/contact/import", permission.MailContactImport)
	permission.Declare("POST", "/api/mail/contact/status", permission.MailContactStatus)
	permission.Declare("POST", "/api/mail/contact/save", permission.MailContactSave)
	permission.Declare("POST", "/api/mail/contact/delete", permission.MailContactDelete)
	permission.Declare("POST", "/api/mail/contact/tag", permission.MailContactTag)
	permission.Declare("POST", "/api/mail/campaign/save", permission.MailCampaignSave)
	permission.Declare("POST", "/api/mail/campaign/start", permission.MailCampaignStart)
	permission.Declare("POST", "/api/mail/campaign/delete", permission.MailCampaignDelete)
	permission.Declare("POST", "/api/mail/automation/save", permission.MailAutomationSave)
	permission.Declare("POST", "/api/mail/automation/status", permission.MailAutomationStatus)
	permission.Declare("POST", "/api/mail/automation/delete", permission.MailAutomationDelete)
	permission.Declare("POST", "/api/mail/automation/tick", permission.MailAutomationTick)
}
