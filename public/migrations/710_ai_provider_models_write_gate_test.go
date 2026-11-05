package migrations_test

import (
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const aiProviderModelsWriteGateVersion = "710-ai-provider-models-write-gate"

// 710 的两条界面词条（令牌状态），中英各一份。
var aiProviderModelsWriteGateWords = map[string]map[string]string{
	"admin.ai.mcp.token.status.active": {
		"zh-CN": "启用",
		"en-US": "Active",
	},
	"admin.ai.mcp.token.status.revoked": {
		"zh-CN": "已撤销",
		"en-US": "Revoked",
	},
}

// 710 的三个按钮节点及其应有的父菜单权限点。
var aiProviderModelsWriteGateButtons = [][2]string{
	{"ai:provider_models_candidates", "ai:provider_list"},
	{"ai:provider_models_append", "ai:provider_list"},
	{"ai:token_toggle", "ai:token_list"},
	{"ai:session_send", "ai:provider_list"},
}

func findAIProviderModelsWriteGateSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, s := range migrations.AllSeeds() {
		if s.Version == aiProviderModelsWriteGateVersion {
			return s
		}
	}
	t.Fatalf("迁移 %s 未注册（register.go 里漏了 registerAIProviderModelsWriteGate？）", aiProviderModelsWriteGateVersion)
	return migrations.Seed{}
}

// seedWriteGateRows 铺齐本批全部产物：四条词条 + 四个按钮节点 + 四条权限关联。
func seedWriteGateRows(t *testing.T, db *gorm.DB, skipButton string, skipLink string) {
	t.Helper()
	for key, langs := range aiProviderModelsWriteGateWords {
		for lang, value := range langs {
			if err := db.Exec(
				"INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) "+
					"VALUES (?, ?, ?, 200, 'ui', 'test', 1, now(), now()) "+
					"ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value",
				key, lang, value).Error; err != nil {
				t.Fatalf("铺词条 %s/%s: %v", key, lang, err)
			}
		}
	}
	for _, btn := range aiProviderModelsWriteGateButtons {
		code, parentCode := btn[0], btn[1]
		if code == skipButton {
			continue
		}
		// 父节点：从已 seed 的二级菜单里按权限点找（与 710 SQL 的定位方式一致）。
		var parentID int64
		if err := db.Raw(
			"SELECT pm.id FROM sys_menus pm JOIN sys_menu_permission mp ON mp.menu_id = pm.id "+
				"WHERE pm.deleted_at IS NULL AND pm.type = 2 AND mp.permission_code = ? "+
				"ORDER BY pm.id LIMIT 1", parentCode).Scan(&parentID).Error; err != nil {
			t.Fatalf("查父菜单 %s: %v", parentCode, err)
		}
		// 空迁移库里没有 ai 的二级菜单（种子由 RunSeeds 写入），本用例自带一份父节点；
		// 470 的触发器会按 permission_code 自动补 sys_menu_permission 关联。
		if parentID == 0 {
			if err := db.Raw(
				"INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, title_key, "+
					"is_system, is_hidden, sort_order, create_by, create_time, update_by, update_time) "+
					"VALUES (?, 0, 2, '', '', ?, ?, 1, 0, 1, 0, now(), 0, now()) RETURNING id",
				parentCode, parentCode, parentCode).Scan(&parentID).Error; err != nil {
				t.Fatalf("铺父菜单 %s: %v", parentCode, err)
			}
		}
		var menuID int64
		if err := db.Raw(
			"INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, title_key, "+
				"is_system, is_hidden, sort_order, create_by, create_time, update_by, update_time) "+
				"VALUES (?, ?, 3, '', '', ?, ?, 1, 0, 90, 0, now(), 0, now()) RETURNING id",
			code, parentID, code, code).Scan(&menuID).Error; err != nil {
			t.Fatalf("铺按钮节点 %s: %v", code, err)
		}
		// 470 的 AFTER INSERT 触发器会按 sys_menus.permission_code 自动补关联，
		// 这里只需保证「不要关联」的场景把触发器写出的那一行删掉。
		if code == skipLink {
			if err := db.Exec(
				"DELETE FROM sys_menu_permission WHERE menu_id = ? AND permission_code = ?",
				menuID, code).Error; err != nil {
				t.Fatalf("移除权限关联 %s: %v", code, err)
			}
		}
	}
}

// TestAIProviderModelsWriteGateConditionCoversAllArtifacts 守门槛的覆盖面。
//
// 这批迁移产出两类东西：菜单按钮节点（含 sys_menu_permission 关联）与两条界面词条。
// 门槛若只查词条，那么「菜单节点没插进去」或「关联漏了」时它照样判为完成，
// SQL 再也不执行 —— 后端允许、界面上却没有按钮，是最难发现的一种半种完。
func TestAIProviderModelsWriteGateConditionCoversAllArtifacts(t *testing.T) {
	seed := findAIProviderModelsWriteGateSeed(t)

	cases := []struct {
		name       string
		dropWords  [][2]string
		skipButton string
		skipLink   string
		want       int64
		label      string
	}{
		{name: "三样全齐", want: 1, label: "已完成，跳过重跑"},
		{
			name:      "缺 en 的 active",
			dropWords: [][2]string{{"admin.ai.mcp.token.status.active", "en-US"}},
			want:      0,
			label:     "英文缺一条也要重跑（只数 zh 会漏掉）",
		},
		{
			name:      "缺两条词条",
			dropWords: [][2]string{{"admin.ai.mcp.token.status.active", "zh-CN"}, {"admin.ai.mcp.token.status.revoked", "en-US"}},
			want:      0,
			label:     "词条不全必须重跑",
		},
		{
			name:       "缺候选按钮节点",
			skipButton: "ai:provider_models_candidates",
			want:       0,
			label:      "菜单节点没插进去 → 界面没有按钮，必须重跑",
		},
		{
			name:       "缺开关按钮节点",
			skipButton: "ai:token_toggle",
			want:       0,
			label:      "同上",
		},
		{
			name:     "缺追加按钮的权限关联",
			skipLink: "ai:provider_models_append",
			want:     0,
			label:    "节点在但没有 sys_menu_permission 关联 → 读路径看不见，必须重跑",
		},
		{
			name:       "缺会话发送按钮节点",
			skipButton: "ai:session_send",
			want:       0,
			label:      "续写会话的按钮也要有节点，否则界面上发不出这条请求",
		},
		{
			name:     "缺会话发送按钮的权限关联",
			skipLink: "ai:session_send",
			want:     0,
			label:    "节点在但没有关联 → 按钮不显示，必须重跑",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db := support.NewMigratedPGTestDB(t)
			if db == nil {
				return
			}
			seedWriteGateRows(t, db, tc.skipButton, tc.skipLink)
			for _, d := range tc.dropWords {
				if err := db.Exec(
					"DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", d[0], d[1]).Error; err != nil {
					t.Fatalf("删除 %s/%s: %v", d[0], d[1], err)
				}
			}

			var applied int64
			if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil {
				t.Fatalf("判据求值失败: %v", err)
			}
			if applied != tc.want {
				t.Fatalf("门槛判据 = %d，期望 %d（%s）", applied, tc.want, tc.label)
			}
		})
	}
}

// TestAIProviderModelsWriteGateWritesAllArtifacts 验实际写入：
// 门槛成立时执行 SQL，词条、按钮节点、权限关联都要落库，且重跑两次不重复插。
func TestAIProviderModelsWriteGateWritesAllArtifacts(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	seed := findAIProviderModelsWriteGateSeed(t)

	for i := 0; i < 2; i++ {
		for _, stmt := range migrations.SplitStatements(seed.SQL) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("执行 710 第 %d 次失败: %v", i+1, err)
			}
		}
	}

	for key, langs := range aiProviderModelsWriteGateWords {
		for lang, want := range langs {
			var got string
			if err := db.Raw(
				"SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).Scan(&got).Error; err != nil {
				t.Fatalf("读 %s/%s: %v", key, lang, err)
			}
			if got != want {
				t.Fatalf("%s/%s 文案应为 %q，实际 %q", key, lang, want, got)
			}
		}
	}

	for _, btn := range aiProviderModelsWriteGateButtons {
		code := btn[0]
		var n int64
		if err := db.Raw(
			"SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND type = 3 AND permission_code = ?",
			code).Scan(&n).Error; err != nil {
			t.Fatalf("数按钮节点 %s: %v", code, err)
		}
		if n != 1 {
			t.Fatalf("按钮节点 %s 应恰好 1 个（重跑不得重复插），实际 %d", code, n)
		}
		var linked int64
		if err := db.Raw(
			"SELECT COUNT(*) FROM sys_menu_permission mp JOIN sys_menus m ON m.id = mp.menu_id "+
				"WHERE m.deleted_at IS NULL AND m.type = 3 AND mp.permission_code = ?", code).Scan(&linked).Error; err != nil {
			t.Fatalf("数权限关联 %s: %v", code, err)
		}
		if linked != 1 {
			t.Fatalf("权限关联 %s 应恰好 1 条，实际 %d", code, linked)
		}
	}
}
