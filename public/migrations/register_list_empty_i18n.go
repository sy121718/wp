package migrations

import "sync"

// register_list_empty_i18n.go — 管理域四个列表页空态文案的迁移（399）。
//
// 见 399_i18n_list_empty_states.sql 的头部：模板里的中文只是 t() 兜底，词条命中时
// 显示的是库里的值；193 的 seed 是 ON CONFLICT DO NOTHING，改模板文案对存量库是 no-op，
// 所以「去掉空态里的『点右上…』」这件事只能由一条新迁移完成（与 316 / 317 同一手法）。
//
// 注册方式：本文件自带 init()（与 register_showcase_blueprints.go 同形）。
// register.go 的 init() 由集成方在合并期统一改动，本票的注册先放在这里 ——
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerListEmptyStateI18n() {
	registerListEmptyStateI18nOnce.Do(registerListEmptyStateI18nSeed)
}

// registerListEmptyStateI18nOnce 让重复调用成为空操作。
var registerListEmptyStateI18nOnce sync.Once

// registerListEmptyStateI18nSeed 注册 399（真正干活的那一半）。
func registerListEmptyStateI18nSeed() {
	// 条数与判定口径：
	//   · 8 = 4 个新增标题词条 × 2 语言的 (item_key, lang) 行都在；
	//   · 4 = 4 个旧描述词条的 zh-CN 值**已不再是旧默认值**（被本迁移改过，或运营手工改过）。
	// 用「<> 旧值」而不是「= 新值」：运营若把描述改成自己的话，这条迁移不该每次启动都重跑
	// （重复执行虽无害，但会一直刷「种子数据完成」的日志，掩盖真正的新迁移）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的值只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "399-i18n-list-empty-states",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 12 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(item_key IN ('admin.admins.empty.title','admin.roles.empty.title','admin.permissions.empty.title','admin.datarules.empty.title') " +
			"AND lang IN ('zh-CN','en-US')) " +
			"OR (item_key = 'admin.admins.empty' AND lang = 'zh-CN' AND item_value <> '还没有管理员，点右上「新建管理员」创建一个。') " +
			"OR (item_key = 'admin.admins.empty' AND lang = 'en-US' AND item_value <> 'No administrators yet. Click \"New administrator\" in the top right to create one.') " +
			"OR (item_key = 'admin.roles.empty' AND lang = 'zh-CN' AND item_value <> '还没有角色，点右上「新建角色」创建一个。') " +
			"OR (item_key = 'admin.roles.empty' AND lang = 'en-US' AND item_value <> 'No roles yet. Click \"New role\" in the top right to create one.') " +
			"OR (item_key = 'admin.permissions.empty' AND lang = 'zh-CN' AND item_value <> '还没有权限点，点右上「新建权限点」创建一个。') " +
			"OR (item_key = 'admin.permissions.empty' AND lang = 'en-US' AND item_value <> 'No permissions yet. Click \"New permission\" in the top right to create one.') " +
			"OR (item_key = 'admin.datarules.empty' AND lang = 'zh-CN' AND item_value <> '还没有数据规则，点右上「新建规则」创建一个。') " +
			"OR (item_key = 'admin.datarules.empty' AND lang = 'en-US' AND item_value <> 'No data rules yet. Click \"New rule\" in the top right to create one.')",
		SQL: mustSQL("399_i18n_list_empty_states.sql"),
	})
}

func init() { registerListEmptyStateI18n() }
