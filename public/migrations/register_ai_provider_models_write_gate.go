package migrations

import "sync"

// registerAIProviderModelsWriteGate 登记 AI 模型目录写入口的按钮节点与令牌状态词条（710）。
//
// 这批只补两样 permission sync 管不到的东西：
//   - sys_menus type=3 的按钮节点与 sys_menu_permission 关联（按钮可见性只看这两张表）；
//   - admin.ai.mcp.token.status.{active,revoked} 的中英词条（模板 tr() 用的 key，
//     之前没有任何 seed，界面上会露出裸 key）。
//
// 权限点行本身由 743-permission-directory-backfill 统一补齐，不在这里重复。
// 幂等：SQL 侧用 NOT EXISTS / ON CONFLICT DO NOTHING。
func registerAIProviderModelsWriteGate() {
	registerAIProviderModelsWriteGateOnce.Do(registerAIProviderModelsWriteGateSeed)
}

var registerAIProviderModelsWriteGateOnce sync.Once

func registerAIProviderModelsWriteGateSeed() {
	registerSeed(Seed{
		Version:   "710-ai-provider-models-write-gate",
		TableName: "sys_i18n",
		// 门槛必须覆盖本批**全部三样**产物，只查词条会让「菜单节点 / 权限关联缺失」
		// 永远被判为已完成、SQL 再也不执行：
		//   ① 两个 key 的中英四条词条（缺一侧语言也算没种完 —— 只数 zh 会让英文缺失被跳过）；
		//   ② 四个 type=3 按钮节点都挂在正确的二级菜单下（父节点错了按钮就飘到树外，
		//      界面看不见、后端却允许，是最难发现的一种「半种完」）；
		//   ③ 四个按钮节点都在 sys_menu_permission 里有关联（读路径只读这张表，见迁移 470）。
		ConditionSQL: `SELECT CASE WHEN
    (SELECT COUNT(*) FROM sys_i18n
      WHERE (item_key, lang) IN (
        ('admin.ai.mcp.token.status.active', 'zh-CN'),
        ('admin.ai.mcp.token.status.active', 'en-US'),
        ('admin.ai.mcp.token.status.revoked', 'zh-CN'),
        ('admin.ai.mcp.token.status.revoked', 'en-US'))) = 4
    AND NOT EXISTS (
        SELECT 1 FROM (VALUES
            ('ai:provider_models_candidates', 'ai:provider_list'),
            ('ai:provider_models_append', 'ai:provider_list'),
            ('ai:token_toggle', 'ai:token_list'),
            ('ai:session_send', 'ai:provider_list')
        ) AS want(code, parent_code)
        WHERE NOT EXISTS (
            SELECT 1 FROM sys_menus m
              JOIN sys_menu_permission mp ON mp.menu_id = m.id AND mp.permission_code = want.code
              JOIN sys_menus p2 ON p2.id = m.parent_id AND p2.deleted_at IS NULL AND p2.type = 2
              JOIN sys_menu_permission pp ON pp.menu_id = p2.id AND pp.permission_code = want.parent_code
             WHERE m.deleted_at IS NULL AND m.type = 3 AND m.permission_code = want.code))
THEN 1 ELSE 0 END`,
		SQL: mustSQL("710_ai_provider_models_write_gate.sql"),
	})
}
