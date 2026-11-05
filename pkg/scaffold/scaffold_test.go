package scaffold

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/plugincomp"
)

func TestFilesGenerateLegalManifest(t *testing.T) {
	files, err := Files("marketing")
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	raw, ok := files["manifest.json"]
	if !ok {
		t.Fatalf("缺 manifest.json")
	}
	m, err := plugincomp.ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("脚手架生成的 manifest 应过校验: %v", err)
	}
	if m.ID != "marketing" || len(m.Components) != 1 || len(m.Presets) != 1 {
		t.Fatalf("manifest 结构错误: %+v", m)
	}
	if m.Migrations != "migrations" || m.SchemaVersion != 1 {
		t.Fatalf("迁移声明错误: %+v", m)
	}

	comp := m.Components[0]
	key := "components/" + comp.Template
	if _, ok := files[key]; !ok {
		t.Fatalf("缺组件模板 %s", key)
	}

	sql := files["migrations/001_init.sql"]
	if !strings.Contains(sql, "CREATE SCHEMA IF NOT EXISTS plugin_marketing") {
		t.Fatalf("迁移 SQL 应含 CREATE SCHEMA: %s", sql)
	}
}

func TestFilesRejectBadName(t *testing.T) {
	for _, name := range []string{"Marketing", "a.b", "a/b", "x", ""} {
		if _, err := Files(name); err == nil {
			t.Fatalf("非法名 %q 应拒绝", name)
		}
	}
}

func TestTitle(t *testing.T) {
	cases := map[string]string{
		"marketing":     "Marketing",
		"member-system": "Member System",
		"coupon_card":   "Coupon Card",
	}
	for in, want := range cases {
		if got := title(in); got != want {
			t.Fatalf("title(%q)=%q want %q", in, got, want)
		}
	}
}
