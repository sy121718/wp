package unit

import (
	json "go_wp/pkg/jsonx"
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

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS block_id_uuid_map (
		old_id    bigint PRIMARY KEY,
		new_id    uuid NOT NULL UNIQUE,
		mapped_at timestamptz NOT NULL DEFAULT now()
	)`).Error; err != nil {
		t.Fatalf("建映射表失败: %v", err)
	}

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

			"VALUES (?::uuid, ?::uuid, 'home', 'none', 'pages/m209/draft.json', ?::jsonb, 1, false, now(), now())",
		pageID, projectID, doc,
	).Error; err != nil {
		t.Fatalf("准备页面文档失败: %v", err)
	}

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
