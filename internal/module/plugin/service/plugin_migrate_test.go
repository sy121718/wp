package pluginservice

import (
	"testing"

	"go_wp/pkg/builder/plugincomp"
)

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

func TestPluginIDWhitelistAcceptsLegal(t *testing.T) {
	m := &plugincomp.Manifest{
		ID: "my-plugin_2", Name: "x", Version: "1.0.0",
		Components: []plugincomp.Component{{Name: "card", Label: "卡片", Template: "card.jet"}},
	}
	if err := plugincomp.ValidateManifest(m); err != nil {
		t.Fatalf("合法插件 ID 应通过校验: %v", err)
	}
}
