package pluginservice

// plugin_patrol_test.go — 巡检分类的纯逻辑单测。
//
// 为什么必须有：**分类错了不报错**。最坏的一种是把正在使用的插件报成孤儿，
// 然后有人照着报告去 DROP 它的 schema 或删它的目录。另一类错误是假告警 ——
// 把 schema_version = 0 的纯组件插件报成「缺 schema」，会让真告警一起被忽略。
// 两种都只在人的判断里体现，测试是唯一能提前拦住的地方。

import (
	"testing"

	pluginmodel "go_wp/internal/module/plugin/model"
)

// existsByID 造一个「这些插件 ID 的目录在磁盘上」的判定函数
// （把文件系统判据注入进来，测试不碰真实磁盘）。
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
		// 纯组件 / 纯模板插件按设计就没有 L1 schema。把它算成「缺 schema」
		// 会制造一整片假告警 —— 假告警会让真告警一起被忽略。
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
				{Name: "plugin_nodir", TableCount: 2}, // 有 schema，只缺目录 —— 用来证明四类互不串味
				{Name: "plugin_orphan", TableCount: 0},
			},
			[]string{"ok", "lost"},
			[]*pluginmodel.Entity{
				{PluginID: "ok", SchemaVersion: 1},     // schema 在 + 目录在 → 全部正常
				{PluginID: "nosche", SchemaVersion: 1}, // 只缺 schema（没有目录需求，判据里给了目录）
				{PluginID: "nodir", SchemaVersion: 1},  // 只缺目录（schema 在 → 不能同时报缺 schema）
			},
			existsByID("ok", "nosche"), // nosche 的目录在，所以它只该出现在「缺 schema」里
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
		// 空切片而非 nil：JSON 里 nil 会变 null，前端要额外判空；
		// 页面模板对 nil 调 len() 虽然安全，但统一形状少一个分支。
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
