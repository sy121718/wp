package migrations_test

// register_mail_automation_definition_v2_test.go — 迁移 621（v1 图定义刷成 v2）的判据。
//
// 三条都要钉住，缺一条这条迁移就会悄悄失效：
//   1. **判据命中**：真实库跑完迁移之后，CheckSQL 必须返回 1（有一条 v1 行残留就会
//      每次启动重跑，而重跑看起来无害、实际把作者正在编辑的行反复重写）；
//   2. **变换正确**：v1 的 next/yes/no 折进 out、branch 改名 filter，end 没有出口；
//   3. **幂等**：把迁移体再跑一遍不报错、不改坏已是 v2 的行。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"

	mailmodel "go_wp/internal/module/mail/model"
)

// mailDefinitionV2Version 与注册文件里的版本号逐字一致（改名时两边都会红）。
const mailDefinitionV2Version = "621-mail-automation-definition-v2"

func findMigration(t *testing.T, version string) migrations.Migration {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == version {
			return m
		}
	}
	t.Fatalf("迁移 %s 未注册", version)
	return migrations.Migration{}
}

// TestMailDefinitionV2MigrationRegistered 注册项的形状：Go 回调而非 SQL，且有内容判据。
func TestMailDefinitionV2MigrationRegistered(t *testing.T) {
	m := findMigration(t, mailDefinitionV2Version)
	if m.Func == nil {
		t.Fatal("621 应使用 Go 回调迁移体（Func），而不是 SQL")
	}
	if m.SQL != "" {
		t.Fatal("621 同时声明了 SQL 与 Func：只能二选一")
	}
	if m.TableName != "mail_automations" {
		t.Fatalf("TableName = %q，期望 mail_automations", m.TableName)
	}
	if m.CheckSQL == "" {
		t.Fatal("621 必须有自定义 CheckSQL：改的是行内容，默认的「表存在」判据会让它永远跳过")
	}
}

// TestMailDefinitionV2MigrationUpgradesRows 造一条 v1 行，跑迁移体，断言形状。
func TestMailDefinitionV2MigrationUpgradesRows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	m := findMigration(t, mailDefinitionV2Version)
	ctx := context.Background()
	model := mailmodel.NewMailModel(db)

	// 直接写一条 v1 行（绕过 service：service 现在保存的就是 v2）。
	v1 := mailmodel.JSONMap{
		"entry": "n1",
		"nodes": []any{
			map[string]any{"key": "n1", "type": "trigger", "next": "n2"},
			map[string]any{"key": "n2", "type": "branch",
				"params": map[string]any{"conditions": []any{"opened"}},
				"yes":    "n3", "no": "n4"},
			map[string]any{"key": "n3", "type": "end"},
			map[string]any{"key": "n4", "type": "end"},
		},
	}
	e := &mailmodel.MailAutomationEntity{
		Name: "v1 存量流程", TriggerType: mailmodel.TriggerManual,
		Definition: v1, Status: mailmodel.AutomationStatusDraft, Version: 1,
	}
	if err := model.CreateAutomation(ctx, e); err != nil {
		t.Fatalf("造 v1 行失败: %v", err)
	}

	// 迁移体的判据此刻必须说「还有 v1 行」。
	var before int64
	if err := db.Raw("SELECT CASE WHEN " + mailV1CountSQLForTest + " = 0 THEN 1 ELSE 0 END").Scan(&before).Error; err != nil {
		t.Fatalf("判据执行失败: %v", err)
	}
	if before != 0 {
		t.Fatalf("迁移前判据应返回 0（还有 v1 行），实际 %d", before)
	}

	// 用迁移器的入口跑一次（同一条纪律：检查 → 同事务执行 → 日志）。
	if err := migrations.ApplyFunc(db, m.Version, m.TableName, m.CheckSQL, m.Func); err != nil {
		t.Fatalf("执行 621 失败: %v", err)
	}

	row, err := model.GetAutomation(ctx, e.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	raw, merr := json.Marshal(row.Definition)
	if merr != nil {
		t.Fatal(merr)
	}
	var def struct {
		Schema int              `json:"schema"`
		Entry  string           `json:"entry"`
		Nodes  []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("升级后的定义不是合法 JSON: %v", err)
	}
	if def.Schema != 2 {
		t.Fatalf("schema = %d，期望 2", def.Schema)
	}
	byKey := map[string]map[string]any{}
	for _, n := range def.Nodes {
		key, _ := n["key"].(string)
		byKey[key] = n
		if _, leftover := n["next"]; leftover {
			t.Fatalf("节点 %s 仍带扁平 next", key)
		}
	}
	n1Out, _ := byKey["n1"]["out"].(map[string]any)
	if n1Out["next"] != "n2" {
		t.Fatalf("n1 的 next 未折进 out: %v", n1Out)
	}
	if typ, _ := byKey["n2"]["type"].(string); typ != "filter" {
		t.Fatalf("branch 应改名为 filter，实际 %v", byKey["n2"]["type"])
	}
	n2Out, _ := byKey["n2"]["out"].(map[string]any)
	if n2Out["yes"] != "n3" || n2Out["no"] != "n4" {
		t.Fatalf("分支出边未折进 out: %v", n2Out)
	}
	params, _ := byKey["n2"]["params"].(map[string]any)
	if params["mode"] != "all" {
		t.Fatalf("branch 升级成 filter 时 mode 应为 all，实际 %v", params["mode"])
	}
	if out, _ := byKey["n3"]["out"].(map[string]any); len(out) != 0 {
		t.Fatalf("end 不该有出口，实际 %v", out)
	}

	// 判据现在必须说「已应用」。
	var after int64
	if err := db.Raw("SELECT CASE WHEN "+mailV1CountSQLForTest+" = 0 THEN 1 ELSE 0 END").Scan(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != 1 {
		t.Fatalf("迁移后判据应返回 1，实际 %d", after)
	}

	// 幂等：再跑一遍不报错，行内容不变。
	if err := migrations.ApplyFunc(db, m.Version, m.TableName, m.CheckSQL, m.Func); err != nil {
		t.Fatalf("第二次执行 621 应跳过而非报错: %v", err)
	}
	again, _ := model.GetAutomation(ctx, e.ID)
	rawAgain, _ := json.Marshal(again.Definition)
	if string(rawAgain) != string(raw) {
		t.Fatalf("第二次执行改动了行内容：\n前: %s\n后: %s", raw, rawAgain)
	}
}

// mailV1CountSQLForTest 与注册文件里的判据逐字一致（只用 SQL 文本，不复制 Go 侧实现）。
// 外层括号是必须的：嵌进 CASE 表达式的子查询不带括号会报 42601。
const mailV1CountSQLForTest = `(SELECT COUNT(*) FROM mail_automations
 WHERE definition IS NOT NULL
   AND jsonb_typeof(definition) = 'object'
   AND btrim(coalesce(definition->>'schema', '')) <> '2')`
