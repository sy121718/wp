package migrations

import "sync"

// registerAIPickerNoPermissionI18n 登记 AI 候选弹窗「没有追加权限」的界面词条（712）。
//
// 这条词条是权限拆分（candidates / append 两个独立权限点）带来的界面分支：
// 没有 ai:provider_models_append 时弹窗不出提交表单、只给一句说明。
func registerAIPickerNoPermissionI18n() {
	registerAIPickerNoPermissionI18nOnce.Do(registerAIPickerNoPermissionI18nSeed)
}

var registerAIPickerNoPermissionI18nOnce sync.Once

func registerAIPickerNoPermissionI18nSeed() {
	registerSeed(Seed{
		Version:   "712-ai-picker-no-permission-i18n",
		TableName: "sys_i18n",
		// 门槛只枚举本批自己的 (item_key, lang) 元组：两个语言全齐才算完成。
		// 用全库计数或 LIKE 前缀会让存量库永远满足，补词条的那条迁移就再也不会执行；
		// 只数 zh-CN 一条也不行 —— en-US 缺的时候门槛照样成立，英文界面就永远缺词。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.ai.picker.noPermission', 'zh-CN'), " +
			"('admin.ai.picker.noPermission', 'en-US'))",
		SQL: mustSQL("712_ai_picker_no_permission_i18n.sql"),
	})
}
