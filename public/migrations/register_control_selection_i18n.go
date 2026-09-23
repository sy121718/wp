package migrations

import "sync"

// register_control_selection_i18n.go — 控件选型收口的词条（416）。
//
// 见 416_i18n_control_selection.sql 的头部：优惠码时间窗由文本输入改成原生
// <input type="datetime-local">、内容模板的实体类型下拉加 <optgroup> 分组、
// 优惠码编辑抽屉补 3 个字段标签。本批新增 5 个 key × 2 语言，并修正 6 个既有 key
// 的文案（旧文案描述的输入方式已随控件更换而消失）。
//
// 新增的 5 个 key（× 2 语言）：
//
//	admin.coupons.detail.ph.max_uses             —— 编辑抽屉「总次数上限」字段标签
//	admin.coupons.detail.ph.per_user_limit       —— 编辑抽屉「每人限次」字段标签
//	admin.coupons.detail.ph.remark               —— 编辑抽屉「备注」字段标签
//	admin.content.templates.entityGroupStructure —— 实体类型下拉的分组标签（结构模板）
//	admin.content.templates.entityGroupContent   —— 实体类型下拉的分组标签（内容实体）
//
// 注册方式：本文件自带 init()（与 register_product_list_filter_i18n.go 同形），
// 不需要在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerControlSelectionI18n() {
	registerControlSelectionI18nOnce.Do(registerControlSelectionI18nSeed)
}

// registerControlSelectionI18nOnce 让重复调用成为空操作。
var registerControlSelectionI18nOnce sync.Once

// registerControlSelectionI18nSeed 注册 416（真正干活的那一半）。
func registerControlSelectionI18nSeed() {
	// 门槛 = 本批 5 个新 key 的 **en-US** 行都在（= 5 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过。
	//
	// 放在**种子**台账而不是迁移台账：本票要修的 6 个 key 由 190（registerSeed）插入，
	// 而迁移台账整体先于种子台账执行 —— 放迁移侧会对新库「先改后插」，UPDATE 一行都打不到
	// （AGENTS.md「Migrations 台账先跑、Seeds 台账后跑」那条的同类场景）。本票 416 > 190，
	// 同台账内按版本升序，先插后改的顺序成立。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行（? 被换成表名），让这条迁移每次启动重跑。
	registerSeed(Seed{
		Version:   "416-i18n-control-selection",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.coupons.detail.ph.max_uses','admin.coupons.detail.ph.per_user_limit'," +
			"'admin.coupons.detail.ph.remark'," +
			"'admin.content.templates.entityGroupStructure','admin.content.templates.entityGroupContent')",
		SQL: mustSQL("416_i18n_control_selection.sql"),
	})
}

func init() { registerControlSelectionI18n() }
