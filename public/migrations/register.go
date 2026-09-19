// 本包注册全部数据库迁移：按 Version 字符串排序执行。
//
// 各迁移通过代表性表名做幂等存在性检查；SQL 文本由 SplitStatements
// 按语句边界（含 DO $$ 块）安全拆分后逐条执行。
//
// 迁移注册体按主题拆到 register_*.go。为什么是这个形状：
//
//  1. 拆分前 170 条注册全部挤在一个 2199 行的 init() 里，新增一条要在巨函数中间
//     找位置插入，review 时 diff 也被整体淹没 —— 这是「手工维护」成本的前一半；
//  2. SQL 原先逐个写成 //go:embed + 包级变量（168 条 embed 指令 + 168 个变量），
//     现在整目录一次嵌入（//go:embed *.sql）、注册时按文件名取 —— 这是后一半。
//     于是新增一条迁移只剩两处改动：新增 NNN_*.sql、在对应主题文件里加一条
//     register/registerSeed；不再需要同步维护变量声明。
//  3. 「写了 SQL 却忘了注册」不再是静默失效（此前 195 未注册导致一批测试在缺列上
//     失败，正是这类遗漏）：TestEmbeddedSQLFilesAllRegistered 对磁盘 .sql 与注册
//     台账做双向一一对应校验。
//
// 执行顺序与注册顺序无关：All()/AllSeeds() 各自按 compareVersion 排序，
// ValidateRegistry 拒绝重复版本号。注册顺序仍按原 init() 的历史顺序逐段保留，
// 使「谁先注册」在 diff 里依然可见。
package migrations

import (
	"embed"
	"fmt"
)

//go:embed *.sql
var migrationSQLFS embed.FS

// registeredSQLFiles 记录被注册项引用过的 SQL 文件名，供一致性测试比对。
var registeredSQLFiles = map[string]struct{}{}

// mustSQL 按文件名取出嵌入的迁移 SQL。
//
// 找不到文件时 panic（发生在 init 阶段，等于启动即失败）：静默返回空 SQL 会让
// 一条迁移退化成「空执行」而程序照常启动 —— 这正是本文件最容易踩的坑。
func mustSQL(name string) string {
	data, err := migrationSQLFS.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("迁移 SQL %s 不存在（//go:embed *.sql 未包含该文件）: %v", name, err))
	}
	registeredSQLFiles[name] = struct{}{}
	return string(data)
}

// init 按主题顺序调用各注册组。
//
// 分组边界沿原 init() 的历史顺序切分（见各 register_*.go 的注释），因此每个
// 切片内的注册相对顺序与拆分前逐条一致。
func init() {
	registerCoreSchemaAndAccess()
	registerMediaAndUniqueConstraints()
	registerCatalogAndInventory()
	registerI18nDataLayer()
	registerIdentityAndMail()
	registerOrderAndSiteSlots()
	registerAnalyticsSeoAndPermissions()
	registerAdminI18nSeedsAndLatest()
	registerPluginPatrolI18n()
	registerBulkNoticeI18n()
	registerAdminRemainingTemplatesI18n()
	// 290：导航菜单页 + 工作台检查器的补漏词条（见 register_navigation_menu_i18n.go）。
	registerNavigationMenuI18n()
	// 291：结构模板后台改造的词条（内容模板列表页的生效 / 引用列 · 主题设置的结构模板下拉）。
	registerContentTemplateStructureI18n()
	// 292：工作台「预览 / 编译 422」的可归因文案（workbench.err.*，见 register_workbench_facing_i18n.go）。
	registerWorkbenchFacingI18n()
	// 293：运行时片段「登录面板」的访客文案（site.fragment.login_panel.*，见 register_fragment_login_panel_i18n.go）。
	registerFragmentLoginPanelI18n()
	// 294：文案词条页的只读权限点 i18n:view（补 178 的读侧缺口，见 register_i18n_view_permission.go）。
	registerI18nViewPermission()
	// 297：块删除保护的引用类别词条（MsgBlockUsage*，见 register_block_usage_i18n.go）。
	registerBlockUsageI18n()
	// 298：商品标签页「命中商品」展开区的词条（审计 PERF-02，见 register_product_tag_hits_i18n.go）。
	registerProductTagHitsI18n()
}
