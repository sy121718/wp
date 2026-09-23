package migrations

import "sync"

// register_product_list_filter_i18n.go — 商品域 4 个后台列表页的筛选 / 分页新增词条（411）。
//
// 见 411_i18n_product_list_filter.sql 的头部：`product_attributes.html` / `product_categories.html`
// / `product_brands.html` / `product_tags.html` 四页此前没有任何关键词入口（`.filter-bar` 计数为 0），
// 其中三页还是全量渲染、属性页被 `Size:200` 静默截断（审计 02-M 的 D12 / D13）。
// 本批在四页上加了筛选栏、分页条与「筛出来是空的」空态档位，新增的是这些文案位的词条。
//
// 共 12 个 key × 2 语言：
//
//	admin.common.filter.submit           —— 筛选栏的提交按钮（四页共用）
//	admin.common.filter.reset            —— 清掉全部筛选条件（筛选栏 + 空态共用）
//	admin.common.filter.optionAll        —— 下拉筛选项的「不过滤」分支
//	admin.common.filter.emptyHint        —— 筛选无结果时的空态描述
//	admin.product_attributes.filter.phKeyword          —— 关键词输入框占位
//	admin.product_attributes.list.emptyFilteredTitle   —— 筛出来是空的（标题）
//	admin.product_categories.filter.phKeyword          —— 同上（分类页）
//	admin.product_categories.empty.filteredTitle       —— 同上（分类页）
//	admin.product_brands.filter.phKeyword              —— 同上（品牌页）
//	admin.product_brands.empty.filteredTitle           —— 同上（品牌页）
//	admin.product_tags.filter.phKeyword                —— 同上（标签页）
//	admin.product_tags.list.emptyFilteredTitle         —— 同上（标签页）
//
// 注册方式：本文件自带 init()（与 register_page_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerProductListFilterI18n() {
	registerProductListFilterI18nOnce.Do(registerProductListFilterI18nSeed)
}

// registerProductListFilterI18nOnce 让重复调用成为空操作。
var registerProductListFilterI18nOnce sync.Once

// registerProductListFilterI18nSeed 注册 411（真正干活的那一半）。
func registerProductListFilterI18nSeed() {
	// 门槛 = 本批 12 个 key 的 **en-US** 行都在（= 12 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行（? 被换成表名），让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "411-i18n-product-list-filter",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.common.filter.submit','admin.common.filter.reset'," +
			"'admin.common.filter.optionAll','admin.common.filter.emptyHint'," +
			"'admin.product_attributes.filter.phKeyword','admin.product_attributes.list.emptyFilteredTitle'," +
			"'admin.product_categories.filter.phKeyword','admin.product_categories.empty.filteredTitle'," +
			"'admin.product_brands.filter.phKeyword','admin.product_brands.empty.filteredTitle'," +
			"'admin.product_tags.filter.phKeyword','admin.product_tags.list.emptyFilteredTitle')",
		SQL: mustSQL("411_i18n_product_list_filter.sql"),
	})
}

func init() { registerProductListFilterI18n() }
