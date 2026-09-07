package pluginservice

// dropSchemaSQL 的注入面锁死测试（docs/06 §8，审计 Medium→Low）：
// DROP SCHEMA 的标识符拼接安全性完全依赖「插件 ID 白名单拒绝引号/反斜杠」，
// 本测试把这一安全前提与语句输出格式同时固化，防止未来放宽白名单或改动
// 拼接逻辑时悄然引入 SQL 注入。

import (
	"testing"

	"go_wp/internal/builder/plugincomp"
)

// TestDropSchemaSQLFormat 锁死 schema 派生与 DROP 语句输出格式。
func TestDropSchemaSQLFormat(t *testing.T) {
	if got := schemaNameFor("my-plugin"); got != "plugin_my-plugin" {
		t.Fatalf("schemaNameFor(%q) = %q, want %q", "my-plugin", got, "plugin_my-plugin")
	}
	got := dropSchemaSQL("my-plugin")
	want := `DROP SCHEMA IF EXISTS "plugin_my-plugin" CASCADE`
	if got != want {
		t.Fatalf("dropSchemaSQL(%q) = %q, want %q", "my-plugin", got, want)
	}
}

// TestPluginIDWhitelistRejectsQuoteChars 锁死注入面前提：任何含双引号/
// 反斜杠/单引号/分号/空格/大写的插件 ID 都必须被 ValidateManifest 拒绝，
// 否则 dropSchemaSQL 的双引号包裹拼接可被注入。
func TestPluginIDWhitelistRejectsQuoteChars(t *testing.T) {
	badIDs := []string{
		`a"b`, `a\b`, `a'b`, `a;b`, "a b", "A", `a"b"`, `plugin" CASCADE; DROP TABLE sys_admin; --`,
	}
	for _, bad := range badIDs {
		m := &plugincomp.Manifest{ID: bad, Name: "x", Version: "1.0.0"}
		if err := plugincomp.ValidateManifest(m); err == nil {
			t.Fatalf("非法插件 ID %q 应被白名单拒绝", bad)
		}
	}
}

// TestPluginIDWhitelistAcceptsLegal 合法 ID 通过校验（负向对照）。
func TestPluginIDWhitelistAcceptsLegal(t *testing.T) {
	m := &plugincomp.Manifest{
		ID: "my-plugin_2", Name: "x", Version: "1.0.0",
		Components: []plugincomp.Component{{Name: "card", Label: "卡片", Template: "card.jet"}},
	}
	if err := plugincomp.ValidateManifest(m); err != nil {
		t.Fatalf("合法插件 ID 应通过校验: %v", err)
	}
}
