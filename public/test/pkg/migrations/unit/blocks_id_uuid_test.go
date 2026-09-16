package unit

// blocks_id_uuid_test.go — 迁移 209（blocks.id 回到 uuid）的文档重写回归。
//
// 空库验不出这件事：迁移里唯一有风险的步骤是「按映射重写文档里的块引用」，
// 而它只在**文档里真的存在旧引用**时才有动作 —— 全新库重放一遍什么都碰不到。
// 所以本用例造出「映射已分配 + 文档里是旧 id」的状态，再执行 209 的 SQL，
// 逐种键形态断言。三种形态都是生产里真实存在的：
//   props.blockId（core.globalref，可嵌在 root 树任意深度）、
//   settings.structure.headerBlockId / footerBlockId（页面级页眉页脚绑定）、
//   settings.slots.*（主题槽位映射：键是槽位名、值是块 id，无法按键名识别）。
//
// 表结构走生产迁移（migrations.Run），不手抄 —— 手抄的表与生产 schema 会静默分叉，
// 而这条迁移恰恰是「列名 / 类型猜错就全错」的那类。

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const blocksIDMigrationVersion = "209-blocks-id-uuid"

func blocksIDMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == blocksIDMigrationVersion {
			return m.SQL
		}
	}
	t.Fatalf("迁移 %s 未注册到 migrations.All()", blocksIDMigrationVersion)
	return ""
}

func TestBlocksIDMigrationRewritesAllBlockRefs(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	const (
		projectID = "11111111-1111-1111-1111-111111111111"
		pageID    = "22222222-2222-2222-2222-222222222222"
		oldID1    = "99001"
		oldID2    = "99002"
		newID1    = "aaaaaaaa-0000-0000-0000-000000000001"
		newID2    = "aaaaaaaa-0000-0000-0000-000000000002"
	)
	support.SeedProjectRow(t, db, projectID, "迁移 209 用例站点")

	// 自建映射表：它由迁移 209 建出、由迁移 211 删除（映射一次性用掉，留着一张永远为空、
	// 没人查的表只会让人以为存在「按旧 id 反查」的能力）。本用例验证的是 209 的**重写逻辑**，
	// 不是这张表的持久存在 —— 所以在这里按 209 的 DDL 建一次。
	// 依赖「迁移跑完还留着它」会让用例与 211 的清理动作互相锁死：删表就必须改测试。
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS block_id_uuid_map (
		old_id    bigint PRIMARY KEY,
		new_id    uuid NOT NULL UNIQUE,
		mapped_at timestamptz NOT NULL DEFAULT now()
	)`).Error; err != nil {
		t.Fatalf("建映射表失败: %v", err)
	}
	// 造「映射已分配」的状态。
	if err := db.Exec(
		"INSERT INTO block_id_uuid_map (old_id, new_id) VALUES (?::bigint, ?::uuid), (?::bigint, ?::uuid)",
		oldID1, newID1, oldID2, newID2,
	).Error; err != nil {
		t.Fatalf("准备映射失败: %v", err)
	}

	doc := `{
      "settings": {
        "layout": {"mode": "full"},
        "structure": {"headerBlockId": "` + oldID1 + `", "footerBlockId": "` + oldID2 + `"},
        "slots": {"announce": "` + oldID1 + `", "notABlock": "hello"}
      },
      "root": [
        {"id": "n1", "type": "core.text", "props": {"text": "hi"}},
        {"id": "n2", "type": "core.globalref", "props": {"blockId": "` + oldID1 + `"},
         "children": [{"id": "n3", "type": "core.globalref", "props": {"blockId": "` + oldID2 + `"}}]}
      ]
    }`

	if err := db.Exec(
		"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, create_time, update_time) "+
			// kind/content_target_type 要满足 pages_content_contract_check：
			// kind='page' 那条分支要求 content_target_id 非空，这里走 site 页面分支（'home' + 'none'）。
			"VALUES (?::uuid, ?::uuid, 'home', 'none', 'pages/m209/draft.json', ?::jsonb, 1, false, now(), now())",
		pageID, projectID, doc,
	).Error; err != nil {
		t.Fatalf("准备页面文档失败: %v", err)
	}

	// 重放 209 的 SQL。此时 blocks.id 已是 uuid，步骤 2/3 会自行跳过，只有重写那步生效。
	for i, stmt := range migrations.SplitStatements(blocksIDMigrationSQL(t)) {
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行第 %d 条迁移语句失败: %v；语句: %s", i+1, err, stmt)
		}
	}

	var raw string
	if err := db.Raw("SELECT draft_document::text FROM pages WHERE id = ?::uuid", pageID).Scan(&raw).Error; err != nil {
		t.Fatalf("读回文档失败: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("文档不是合法 JSON（重写把它写坏了）: %v", err)
	}

	settings := got["settings"].(map[string]any)
	structure := settings["structure"].(map[string]any)
	if structure["headerBlockId"] != newID1 {
		t.Errorf("settings.structure.headerBlockId 未重写: %v", structure["headerBlockId"])
	}
	if structure["footerBlockId"] != newID2 {
		t.Errorf("settings.structure.footerBlockId 未重写: %v", structure["footerBlockId"])
	}
	slots := settings["slots"].(map[string]any)
	if slots["announce"] != newID1 {
		t.Errorf("settings.slots.announce 未重写: %v", slots["announce"])
	}
	if slots["notABlock"] != "hello" {
		t.Errorf("slots 里未命中映射的值不该被改动: %v", slots["notABlock"])
	}

	root := got["root"].([]any)
	first := root[1].(map[string]any)
	if first["props"].(map[string]any)["blockId"] != newID1 {
		t.Errorf("root[1].props.blockId 未重写: %v", first["props"])
	}
	nested := first["children"].([]any)[0].(map[string]any)
	if nested["props"].(map[string]any)["blockId"] != newID2 {
		t.Errorf("嵌套两层的 blockId 未重写（递归失效）: %v", nested["props"])
	}
	if root[0].(map[string]any)["props"].(map[string]any)["text"] != "hi" {
		t.Errorf("无关节点被改动")
	}
	if strings.Contains(raw, oldID1) || strings.Contains(raw, oldID2) {
		t.Errorf("文档里仍残留旧块 id: %s", raw)
	}
}
