package scaffold

// 脚手架产物合法性测试：生成即合法——manifest 通过 plugincomp.ParseManifest
//（含组件/预设/迁移声明校验），模板文件名符合 components/{template}.jet 约定，
// 迁移 SQL 含 CREATE SCHEMA。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/plugincomp"
)

// TestFilesGenerateLegalManifest 生成的 manifest 过插件校验（端到端验收脚手架）。
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
	// 组件模板文件必须存在且命名匹配 components/{template}.jet。
	comp := m.Components[0]
	key := "components/" + comp.Template
	if _, ok := files[key]; !ok {
		t.Fatalf("缺组件模板 %s", key)
	}
	// 迁移 SQL 含 CREATE SCHEMA（docs/06 §8 约定）。
	sql := files["migrations/001_init.sql"]
	if !strings.Contains(sql, "CREATE SCHEMA IF NOT EXISTS plugin_marketing") {
		t.Fatalf("迁移 SQL 应含 CREATE SCHEMA: %s", sql)
	}
}

// TestFilesRejectBadName 非法插件名拒绝（防目录穿越/大写）。
func TestFilesRejectBadName(t *testing.T) {
	for _, name := range []string{"Marketing", "a.b", "a/b", "x", ""} {
		if _, err := Files(name); err == nil {
			t.Fatalf("非法名 %q 应拒绝", name)
		}
	}
}

// TestTitle 显示名分词首字母大写。
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
