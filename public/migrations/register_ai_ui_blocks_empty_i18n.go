package migrations

import "sync"

// registerAIUIBlocksEmptyI18n 登记 AI 视图块空态词条（709）。
//
// 一条 key（admin.ai.blocks.empty）服务四类块（stat / table / list / accordion）：
// 它们的空态语义完全一致（这块没有数据），按类型拆四条只是把一个事实抄四遍。
// category 记 ui —— 它是界面文案，不是错误声明，不进任何 enums。
// 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
func registerAIUIBlocksEmptyI18n() {
	registerAIUIBlocksEmptyI18nOnce.Do(registerAIUIBlocksEmptyI18nSeed)
}

var registerAIUIBlocksEmptyI18nOnce sync.Once

func registerAIUIBlocksEmptyI18nSeed() {
	registerSeed(Seed{
		Version:   "709-ai-ui-blocks-empty-i18n",
		TableName: "sys_i18n",
		// 门槛精确枚举本批 2 个 (item_key, lang) 组合：中英各一条、缺一不可。
		// 只数 zh 会让「英文缺失」也判为已存在，seed 被跳过、英文界面就没有空态文案。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.ai.blocks.empty', 'zh-CN'), " +
			"('admin.ai.blocks.empty', 'en-US'))",
		SQL: mustSQL("709_ai_ui_blocks_empty_i18n.sql"),
	})
}
