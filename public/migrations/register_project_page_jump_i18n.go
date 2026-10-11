package migrations

import "sync"

// register_project_page_jump_i18n.go — project 域后台页写动作成功回执的新增词条（590）。
//
// 见 590_project_page_jump_i18n.sql 的头部：写动作结论改成 shell.RenderJump 渲染整页
// 提示后，主题的新建 / 激活 / 删除需要一个成功回执（此前是静默 303）。
//
// 本批新增 3 个 key × 2 语言（admin.theme.ok.created / activated / deleted）。
// 站点设置 / 语言清单 / 主题设置的成功回执复用既有词条（058 / 447 / 187 / 451），不重复登记。
//
// 注册方式：本文件自带 init()（与 register_project_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerProjectPageJumpI18n() {
	registerProjectPageJumpI18nOnce.Do(registerProjectPageJumpI18nSeed)
}

// registerProjectPageJumpI18nOnce 让重复调用成为空操作。
var registerProjectPageJumpI18nOnce sync.Once

// registerProjectPageJumpI18nSeed 注册 590（真正干活的那一半）。
func registerProjectPageJumpI18nSeed() {
	// 门槛 = 本批 3 个 key × 中英两种语言 = **6 个 (item_key, lang) 组合都在**（枚举本批自己的
	// 对象，上界封闭）。
	//
	// 为什么两种语言都数：只数一种（这里原先是 en-US）时，另一种语言缺行占不到计数里 ——
	// 任一条 zh-CN 缺失时判据仍返回 1 → RunSeeds 跳过 → 缺的中文**永远补不上**，
	// 界面回退到组件包内的兜底文案（错得安静：没有异常、没有日志）。定则见 AGENTS.md
	// §数据库「判据的偏差方向要刻意选**宁可重跑，不可静默跳过**」。
	// 阈值取**精确组合数 6**：低于它缺词也能满足（同一条缺陷），高于它判据永不满足、
	// 每次启动都重跑这批 SQL。SQL 是 ON CONFLICT (item_key, lang) DO NOTHING，重跑只补缺行、
	// 不覆盖运营改过的值。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能
	// 写成 SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑。
	// 用例：public/migrations/recent_write_ui_seed_guard_test.go。
	registerSeed(Seed{
		Version:   "590-project-page-jump-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.theme.ok.created','admin.theme.ok.activated','admin.theme.ok.deleted')",
		SQL: mustSQL("590_project_page_jump_i18n.sql"),
	})
}

func init() { registerProjectPageJumpI18n() }
