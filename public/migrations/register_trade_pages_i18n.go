package migrations

import "sync"

// register_trade_pages_i18n.go — 交易域后台页词条的修正与新增（417）。
//
// 见 417_fix_trade_pages_i18n.sql 的头部：本批修的四件事都在**库值**上，
// 只改模板等于没改（模板里的中文只是 t(key, 兜底) 的 fallback，词条命中时显示库里的值）：
//
//	① orders 空态「台前下单后（或后台代客建单）就会出现在这里」—— /admin/orders 没有建单端点；
//	② customers 空态标题「没有符合条件的客户」—— 无筛选时自相矛盾；
//	③ customers 空态说明恢复「后台不能直接新建」（旧库值把这条唯一有价值的约束换成了「点重置」）；
//	④ 注册时间范围的两个 label（「注册时间」+「至」→「注册时间（起）」+「注册时间（止）」）。
//
// 另新增 8 个 key × 2 语言（动词 + 退货单状态说明），文案源在
// internal/module/user/inbound/http/customer_handle.go 与
// internal/module/order/inbound/http/return_page_query.go。
//
// 注册方式：本文件自带 init()（与 register_order_page_err_i18n.go /
// register_project_page_err_i18n.go 同形）。init 的注册顺序不影响执行顺序 ——
// 种子按版本号排序，RunSeeds 在所有结构迁移之后执行。
func registerTradePagesI18n() {
	registerTradePagesI18nOnce.Do(registerTradePagesI18nSeed)
}

// registerTradePagesI18nOnce 让重复调用成为空操作。
var registerTradePagesI18nOnce sync.Once

// registerTradePagesI18nSeed 注册 417（真正干活的那一半）。
func registerTradePagesI18nSeed() {
	// 门槛 = 16 条：8 条「被修正的 key 的值已不是旧值」（4 个 key，其中两个 key 的 en-US
	// 行没写进判定 —— 它们与 zh-CN 行同批同一条 UPDATE 修正，少列一条不影响判定强度）
	// + 8 条「新增 key 的 en-US 行都在」。
	//
	// 判据用 `item_value <> '<旧值>'` 而不是 `= '<新值>'`：运营在后台手工改过文案时
	// 也算本批已落地（seed 是默认值来源、后台是真相来源），不该每次都重跑这条迁移。
	// 新增 key 用 en-US 行计数：它们本来就只有本批这一份来源，en-US 在即本批已落库
	//（与 406 判据同口径）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 与文案
	// 只能写成 SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "417-i18n-trade-pages-fix",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 16 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			// 被修正的 4 个 key（按 lang 各算一条，共 8 条）
			"(item_key = 'admin.orders.list.empty_tail' AND lang = 'zh-CN' AND item_value <> '台前下单后（或后台代客建单）就会出现在这里。若已经下过单，检查上面的筛选条件是不是过窄了。') OR " +
			"(item_key = 'admin.orders.list.empty_tail' AND lang = 'en-US' AND item_value <> 'orders appear here once customers check out (or an admin places one on their behalf). If orders do exist, check whether the filters above are too narrow.') OR " +
			"(item_key = 'admin.customers.list.empty_heading' AND lang = 'zh-CN' AND item_value <> '没有符合条件的客户') OR " +
			"(item_key = 'admin.customers.list.empty_heading' AND lang = 'en-US' AND item_value <> 'No matching customers') OR " +
			"(item_key = 'admin.customers.list.empty_desc' AND lang = 'zh-CN' AND item_value <> '站点还没有访客注册，或者上面的筛选条件太窄了 —— 先点「重置」看一眼全部账号。') OR " +
			"(item_key = 'admin.customers.list.empty_desc' AND lang = 'en-US' AND item_value <> 'No visitor has signed up yet, or the filters are too narrow - hit Reset to see every account.') OR " +
			"(item_key = 'admin.customers.field.registered_at' AND lang = 'zh-CN' AND item_value <> '注册时间') OR " +
			"(item_key = 'admin.customers.field.registered_to' AND lang = 'zh-CN' AND item_value <> '至') OR " +
			// 新增的 8 个 key（en-US 行在即视为已落库）
			"(item_key IN ('admin.customers.action.disable','admin.customers.action.enable'," +
			"'admin.returns.note.requested','admin.returns.note.approved','admin.returns.note.received'," +
			"'admin.returns.note.completed','admin.returns.note.rejected','admin.returns.note.cancelled') AND lang = 'en-US')",
		SQL: mustSQL("417_fix_trade_pages_i18n.sql"),
	})
}

func init() { registerTradePagesI18n() }
