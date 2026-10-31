package pluginservice

import (
	"testing"

	pluginmodel "go_wp/internal/module/plugin/model"
)

func existsByID(ids ...string) func(*pluginmodel.Entity) bool {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return func(r *pluginmodel.Entity) bool {
		if r == nil {
			return false
		}
		return set[r.PluginID]
	}
}

func TestClassifyPatrol(t *testing.T) {
	t.Run("全部对得上时不报任何不一致", func(t *testing.T) {
		res := classifyPatrol(
			[]pluginmodel.SchemaInfo{{Name: "plugin_hello", TableCount: 2}},
			[]string{"hello"},
			[]*pluginmodel.Entity{{PluginID: "hello", SchemaVersion: 1}},
			existsByID("hello"),
		)
		if len(res.OrphanSchemas) != 0 || len(res.MissingSchemas) != 0 ||
			len(res.OrphanStorage) != 0 || len(res.MissingStorage) != 0 {
			t.Fatalf("正常插件不应产生任何不一致：%+v", res)
		}
	})

	t.Run("有 schema 无注册行 = 孤儿 schema", func(t *testing.T) {
		res := classifyPatrol(
			[]pluginmodel.SchemaInfo{{Name: "plugin_gone", TableCount: 3}},
			nil, nil, existsByID(),
		)
		if len(res.OrphanSchemas) != 1 || res.OrphanSchemas[0].Name != "plugin_gone" || res.OrphanSchemas[0].TableCount != 3 {
			t.Fatalf("应报出孤儿 schema 及其表数量，实际 %+v", res.OrphanSchemas)
		}
		if len(res.MissingSchemas) != 0 {
			t.Fatalf("没有注册行就不该有缺 schema 的报告，实际 %v", res.MissingSchemas)
		}
	})

	t.Run("有注册行无 schema = 缺 schema", func(t *testing.T) {
		res := classifyPatrol(nil, nil,
			[]*pluginmodel.Entity{{PluginID: "broken", SchemaVersion: 2}},
			existsByID("broken"))
		if len(res.OrphanSchemas) != 0 {
			t.Fatalf("没有多余 schema 就不该报孤儿，实际 %+v", res.OrphanSchemas)
		}
		if len(res.MissingSchemas) != 1 || res.MissingSchemas[0] != "broken" {
			t.Fatalf("应报出缺 schema 的插件 ID，实际 %v", res.MissingSchemas)
		}
	})

	t.Run("schema_version=0 的插件不要求有 schema", func(t *testing.T) {

		res := classifyPatrol(nil, nil,
			[]*pluginmodel.Entity{{PluginID: "pure", SchemaVersion: 0}},
			existsByID("pure"))
		if len(res.MissingSchemas) != 0 || len(res.OrphanSchemas) != 0 {
			t.Fatalf("schema_version=0 不应要求 schema：%+v", res)
		}
	})

	t.Run("有目录无注册行 = 孤儿目录", func(t *testing.T) {
		res := classifyPatrol(nil,
			[]string{"leftover", "hello"},
			[]*pluginmodel.Entity{{PluginID: "hello", SchemaVersion: 1}},
			existsByID("hello"))
		if len(res.OrphanStorage) != 1 || res.OrphanStorage[0] != "leftover" {
			t.Fatalf("只该把 leftover 报成孤儿目录，实际 %v", res.OrphanStorage)
		}
	})

	t.Run("有注册行无目录 = 目录缺失", func(t *testing.T) {
		res := classifyPatrol(nil, nil,
			[]*pluginmodel.Entity{{PluginID: "dirless", SchemaVersion: 0}},
			existsByID())
		if len(res.MissingStorage) != 1 || res.MissingStorage[0] != "dirless" {
			t.Fatalf("应报出目录缺失的插件 ID，实际 %v", res.MissingStorage)
		}
	})

	t.Run("四类同时存在且互不串味", func(t *testing.T) {
		res := classifyPatrol(
			[]pluginmodel.SchemaInfo{
				{Name: "plugin_ok", TableCount: 1},
				{Name: "plugin_nodir", TableCount: 2},
				{Name: "plugin_orphan", TableCount: 0},
			},
			[]string{"ok", "lost"},
			[]*pluginmodel.Entity{
				{PluginID: "ok", SchemaVersion: 1},
				{PluginID: "nosche", SchemaVersion: 1},
				{PluginID: "nodir", SchemaVersion: 1},
			},
			existsByID("ok", "nosche"),
		)
		if len(res.OrphanSchemas) != 1 || res.OrphanSchemas[0].Name != "plugin_orphan" {
			t.Fatalf("孤儿 schema 应为 plugin_orphan，实际 %+v", res.OrphanSchemas)
		}
		if len(res.MissingSchemas) != 1 || res.MissingSchemas[0] != "nosche" {
			t.Fatalf("缺 schema 应为 nosche，实际 %v", res.MissingSchemas)
		}
		if len(res.OrphanStorage) != 1 || res.OrphanStorage[0] != "lost" {
			t.Fatalf("孤儿目录应为 lost，实际 %v", res.OrphanStorage)
		}
		if len(res.MissingStorage) != 1 || res.MissingStorage[0] != "nodir" {
			t.Fatalf("目录缺失应为 nodir，实际 %v", res.MissingStorage)
		}
	})

	t.Run("nil 行被跳过且返回非 nil 空切片", func(t *testing.T) {

		res := classifyPatrol(nil, nil, []*pluginmodel.Entity{nil}, existsByID())
		if res.OrphanSchemas == nil || res.MissingSchemas == nil ||
			res.OrphanStorage == nil || res.MissingStorage == nil {
			t.Fatalf("应返回非 nil 空切片：%+v", res)
		}
		if len(res.MissingStorage) != 0 {
			t.Fatalf("nil 行不该产生报告，实际 %v", res.MissingStorage)
		}
	})
}
