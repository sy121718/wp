package migrations

import "sync"

// register_order_page_err_i18n.go — order 域后台列表页「装载失败」空态的新增词条（406）。
//
// 见 406_i18n_order_page_err.sql 的头部：本批把订单 / 优惠码 / 退货入库三个列表页的
// `c.String(500, orderenums.ErrInternal)`（纯文本 + 硬编码中文）改成降级渲染
//（空列表 + 归口提示 + HTTP 200 + 页面结构完好），随之需要一个「这一页没读出来」的空态档位 ——
// 否则模板只能把「装载失败」显示成「还没有站点工程」/「这个工程还没有订单」，把用户引向建工程 / 改筛选。
//
// 本批新增 2 个 key × 2 语言（三个页面共用同一档空态）：
//   admin.common.list.loadFailedTitle —— 空态标题
//   admin.common.list.loadFailedDesc  —— 空态说明
// 归口提示本身（MsgInternalError）不新增：zh-CN 见 058、en-US 见 405。
//
// 注册方式：本文件自带 init()（与 register_project_page_err_i18n.go /
// register_product_inventory_empty_i18n.go 同形）。init 的注册顺序不影响执行顺序 ——
// 种子按版本号排序，RunSeeds 在所有结构迁移之后执行。
func registerOrderPageErrI18n() {
	registerOrderPageErrI18nOnce.Do(registerOrderPageErrI18nSeed)
}

// registerOrderPageErrI18nOnce 让重复调用成为空操作。
var registerOrderPageErrI18nOnce sync.Once

// registerOrderPageErrI18nSeed 注册 406（真正干活的那一半）。
func registerOrderPageErrI18nSeed() {
	// 门槛 = 本批 2 个 key 的 **en-US** 行都在（说明本批已落库，无需再插）。
	// 挑 en-US 而不是 zh-CN 是有意的：这两个 key 在别处没有 zh-CN 行，但 405 给
	// MsgInternalError 补 en-US 与本批同属「同一类缺口」的批次，按 en-US 计数与本批
	// 「英文界面缺文案」的判据一致。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "406-i18n-order-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.common.list.loadFailedTitle','admin.common.list.loadFailedDesc')",
		SQL: mustSQL("406_i18n_order_page_err.sql"),
	})
}

func init() { registerOrderPageErrI18n() }
