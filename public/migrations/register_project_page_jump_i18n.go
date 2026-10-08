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
	// 门槛 = 本批 3 个 key 的 **en-US** 行都在（枚举本批自己的对象，上界封闭）。
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能
	// 写成 SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑。
	registerSeed(Seed{
		Version:   "590-project-page-jump-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.theme.ok.created','admin.theme.ok.activated','admin.theme.ok.deleted')",
		SQL: mustSQL("590_project_page_jump_i18n.sql"),
	})
}

func init() { registerProjectPageJumpI18n() }
