package mailservice

// automation_definition_test.go — 图定义 v2 的归一化与出口名表（任务卡 T1）。
//
// 这一层的两条判据都不能靠「读代码看得出来」：
//
//  1. **v1 → v2 的升级必须无损**：库里存的是扁平 next/yes/no，读出来要是 out，
//     而 out 里的目标一个都不能丢（丢了的表现是「某条分支没了」，图校验不报错、
//     执行器把实例判失败）。
//  2. **入参不能被改**：normalizeDefinition 会在列表页被调用（一次读一批），
//     改入参等于悄悄改调用方手里的数据。

import (
	"reflect"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// TestNormalizeDefinition 12 例：覆盖 v1/v2/畸形输入与深拷贝。
func TestNormalizeDefinition(t *testing.T) {
	t.Run("v1 单链", func(t *testing.T) {
		raw := map[string]any{
			"entry": "n1",
			"nodes": []any{
				map[string]any{"key": "n1", "type": "trigger", "next": "n2"},
				map[string]any{"key": "n2", "type": "end"},
			},
		}
		got, schema := normalizeDefinition(raw)
		if schema != automationSchemaV2 {
			t.Fatalf("目标 schema = %d，期望 %d", schema, automationSchemaV2)
		}
		if got["schema"] != automationSchemaV2 {
			t.Fatalf("输出 schema = %v，期望 %d", got["schema"], automationSchemaV2)
		}
		nodes := got["nodes"].([]any)
		first := nodes[0].(map[string]any)
		if first["type"] != NodeTypeTrigger {
			t.Fatalf("type 被改写了: %v", first["type"])
		}
		out, ok := first["out"].(map[string]any)
		if !ok || out[outletNext] != "n2" {
			t.Fatalf("next 未折进 out: %v", first["out"])
		}
		if _, leftover := first["next"]; leftover {
			t.Fatalf("v1 的扁平 next 必须消失（v1 与 v2 不得共存），实际 %v", first["next"])
		}
		second := nodes[1].(map[string]any)
		if out, ok := second["out"].(map[string]any); !ok || len(out) != 0 {
			t.Fatalf("end 的 out 必须是空 map，实际 %v", second["out"])
		}
	})

	t.Run("v1 分支升级成 filter", func(t *testing.T) {
		raw := map[string]any{
			"entry": "n1",
			"nodes": []any{
				map[string]any{"key": "n1", "type": "branch",
					"params": map[string]any{"conditions": []any{"opened"}},
					"yes":    "n2", "no": "n3"},
			},
		}
		got, _ := normalizeDefinition(raw)
		node := got["nodes"].([]any)[0].(map[string]any)
		if node["type"] != NodeTypeFilter {
			t.Fatalf("branch 应改名为 filter，实际 %v", node["type"])
		}
		out := node["out"].(map[string]any)
		if out[outletYes] != "n2" || out[outletNo] != "n3" {
			t.Fatalf("分支两条出边未折进 out: %v", out)
		}
		params := node["params"].(map[string]any)
		if params["mode"] != ConditionModeAll {
			t.Fatalf("v1 的 branch 缺 mode 时应补 all（v1 是 AND 语义），实际 %v", params["mode"])
		}
	})

	t.Run("v1 end 没有出口", func(t *testing.T) {
		raw := map[string]any{"entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": "end", "next": "n2"},
		}}
		got, _ := normalizeDefinition(raw)
		node := got["nodes"].([]any)[0].(map[string]any)
		if out := node["out"].(map[string]any); len(out) != 0 {
			t.Fatalf("end 不该接受出边（出口表里它没有出口），实际 %v", out)
		}
	})

	t.Run("v1 缺 next 留空 out", func(t *testing.T) {
		raw := map[string]any{"entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": "trigger"},
		}}
		got, _ := normalizeDefinition(raw)
		node := got["nodes"].([]any)[0].(map[string]any)
		if out := node["out"].(map[string]any); len(out) != 0 {
			t.Fatalf("没有 next 时 out 应为空 map（不入空串），实际 %v", out)
		}
	})

	t.Run("v2 原样保留", func(t *testing.T) {
		raw := map[string]any{
			"schema": float64(2),
			"entry":  "n1",
			"nodes": []any{
				map[string]any{"key": "n1", "type": "filter",
					"params": map[string]any{
						"mode":       ConditionModeAny,
						"conditions": []any{"opened"},
					},
					"out": map[string]any{"yes": "n2", "no": "n3"}},
			},
		}
		got, _ := normalizeDefinition(raw)
		node := got["nodes"].([]any)[0].(map[string]any)
		out := node["out"].(map[string]any)
		if out[outletYes] != "n2" || out[outletNo] != "n3" {
			t.Fatalf("v2 的 out 被改动了: %v", out)
		}
		params := node["params"].(map[string]any)
		if params["mode"] != ConditionModeAny {
			t.Fatalf("v2 已显式声明的 mode 不该被覆盖，实际 %v", params["mode"])
		}
	})

	t.Run("v2 带未知出口名原样透传", func(t *testing.T) {
		// 未知出口是在 nodeArity 报错的，归一化只做形状升级 —— 在这里就筛掉的话
		// 校验器永远看不到它，用户拿到的是一句「走不到 / 悬空边」的间接症状。
		raw := map[string]any{
			"schema": float64(2), "entry": "n1",
			"nodes": []any{map[string]any{
				"key": "n1", "type": "trigger",
				"out": map[string]any{"yse": "n2"},
			}},
		}
		got, _ := normalizeDefinition(raw)
		node := got["nodes"].([]any)[0].(map[string]any)
		if node["out"].(map[string]any)["yse"] != "n2" {
			t.Fatalf("未知出口必须原样透传给校验器，实际 %v", node["out"])
		}
	})

	t.Run("缺 schema 按 v1 处理", func(t *testing.T) {
		raw := map[string]any{"entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": "trigger", "next": "n2"},
		}}
		_, source := normalizeDefinitionVersioned(raw)
		if source != automationSchemaV1 {
			t.Fatalf("缺 schema 的输入应被认作 v1，实际 %d", source)
		}
	})

	t.Run("schema=1 但已带 out", func(t *testing.T) {
		// 半升级的行：声明 v1 却已经有 out。以 out 为准（不拿扁平字段覆盖它），
		// 否则一次「先升级字段、后改 schema」的中间态会把边弄丢。
		raw := map[string]any{
			"schema": float64(1), "entry": "n1",
			"nodes": []any{map[string]any{
				"key": "n1", "type": "trigger", "next": "old",
				"out": map[string]any{"next": "new"},
			}},
		}
		got, _ := normalizeDefinition(raw)
		node := got["nodes"].([]any)[0].(map[string]any)
		if node["out"].(map[string]any)[outletNext] != "new" {
			t.Fatalf("已有 out 时不该被扁平 next 覆盖，实际 %v", node["out"])
		}
	})

	t.Run("空 nodes", func(t *testing.T) {
		got, _ := normalizeDefinition(map[string]any{"entry": "n1"})
		if got["schema"] != automationSchemaV2 {
			t.Fatalf("空 nodes 也要升级 schema，实际 %v", got["schema"])
		}
		if _, ok := got["nodes"]; ok {
			t.Fatalf("原始定义没有 nodes 时不该凭空造一个，实际 %v", got["nodes"])
		}
	})

	t.Run("空定义", func(t *testing.T) {
		got, _ := normalizeDefinition(nil)
		if len(got) != 0 {
			t.Fatalf("空定义归一化后仍是空 map，实际 %v", got)
		}
	})

	t.Run("重复 key 透传", func(t *testing.T) {
		raw := map[string]any{"entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": "end"},
			map[string]any{"key": "n1", "type": "end"},
		}}
		got, _ := normalizeDefinition(raw)
		nodes := got["nodes"].([]any)
		if len(nodes) != 2 {
			t.Fatalf("重复 key 必须原样透传（由校验器报错），实际 %d 个节点", len(nodes))
		}
	})

	t.Run("未知 type 透传", func(t *testing.T) {
		raw := map[string]any{"entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": "teleport"},
		}}
		got, _ := normalizeDefinition(raw)
		node := got["nodes"].([]any)[0].(map[string]any)
		if node["type"] != "teleport" {
			t.Fatalf("未知 type 必须原样透传，实际 %v", node["type"])
		}
	})

	t.Run("深拷贝不改入参", func(t *testing.T) {
		raw := map[string]any{
			"entry": "n1",
			"nodes": []any{map[string]any{
				"key": "n1", "type": "trigger", "next": "n2",
				"params": map[string]any{"nested": map[string]any{"k": "v"}},
			}},
		}
		before := deepCopyJSON(raw)
		got, _ := normalizeDefinition(raw)
		if !reflect.DeepEqual(raw, before) {
			t.Fatalf("归一化改了入参：\n前: %#v\n后: %#v", before, raw)
		}
		// 输出与入参也不能共享子对象：否则改输出会反写调用方的数据。
		node := got["nodes"].([]any)[0].(map[string]any)
		node["params"].(map[string]any)["nested"].(map[string]any)["k"] = "changed"
		if raw["nodes"].([]any)[0].(map[string]any)["params"].(map[string]any)["nested"].(map[string]any)["k"] != "v" {
			t.Fatal("归一化输出与入参共享了子对象（深拷贝失效）")
		}
	})
}

// TestParseDefinitionUpgradesV1 v1 定义经 ParseDefinition 之后全链路只见 out。
func TestParseDefinitionUpgradesV1(t *testing.T) {
	def, err := ParseDefinition(map[string]any{
		"entry": "n1",
		"nodes": []any{
			map[string]any{"key": "n1", "type": "trigger", "next": "n2"},
			map[string]any{"key": "n2", "type": "branch",
				"params": map[string]any{"conditions": []any{"opened"}},
				"yes":    "n3", "no": "n3"},
			map[string]any{"key": "n3", "type": "end"},
		},
	})
	if err != nil {
		t.Fatalf("v1 定义应能读: %v", err)
	}
	if def.Schema != automationSchemaV2 {
		t.Fatalf("解析结果 schema = %d，期望 %d", def.Schema, automationSchemaV2)
	}
	byKey := map[string]AutomationNode{}
	for _, n := range def.Nodes {
		byKey[n.Key] = n
	}
	if byKey["n1"].outlet(outletNext) != "n2" {
		t.Fatalf("n1 的 next 读不到，out=%v", byKey["n1"].Out)
	}
	if byKey["n2"].Type != NodeTypeFilter {
		t.Fatalf("branch 应升级成 filter，实际 %s", byKey["n2"].Type)
	}
	if got := byKey["n2"].outletNames(); !reflect.DeepEqual(got, []string{outletNo, outletYes}) {
		t.Fatalf("n2 的出口名 = %v，期望 [no yes]", got)
	}
}

// TestNodeOutletsTable 出口名表本身：终局形状（10 种类型）。
func TestNodeOutletsTable(t *testing.T) {
	cases := map[string][]string{
		NodeTypeTrigger:          {outletNext},
		NodeTypeDelay:            {outletNext},
		NodeTypeEmail:            {outletNext},
		NodeTypeTag:              {outletNext},
		NodeTypeWebhook:          {outletNext},
		NodeTypeFilter:           {outletYes, outletNo},
		NodeTypeListStatusBranch: {outletNotInList, outletActive, outletNonActive},
		NodeTypeEnd:              {},
	}
	for typ, want := range cases {
		got := nodeOutlets(typ)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s 的出口名 = %v，期望 %v", typ, got, want)
		}
	}
	// ab_test 的出口是动态变体 id，不能写死。
	if got := nodeOutlets(NodeTypeABTest); len(got) != 0 {
		t.Errorf("ab_test 不该有固定出口名，实际 %v", got)
	}
	// v1 的 branch 必须映射到 filter 的出口名（存量行走这条）。
	if got := nodeOutlets(NodeTypeBranch); !reflect.DeepEqual(got, []string{outletYes, outletNo}) {
		t.Errorf("branch 应复用 filter 的出口名，实际 %v", got)
	}
}

// TestUnknownOutletRejected 拼错出口名必须报错（不是「边静默消失」）。
func TestUnknownOutletRejected(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		{"key": "n1", "type": NodeTypeTrigger, "out": map[string]any{"yse": "n2"}},
		{"key": "n2", "type": NodeTypeEnd, "out": map[string]any{}},
	})
	if err == nil {
		t.Fatal("未知出口名应被拒绝")
	}
	if !strings.Contains(err.Error(), "未知出口名") {
		t.Fatalf("错误应说明「未知出口名」，实际 %v", err)
	}
}

// TestAutomationItemProjectsLegacyNodeType dto 投影必须把 v2 的 filter 翻回 v1 的 branch。
//
// 为什么值得单测：画布 JS、表单还原、MCP 工具都按 v1 的名字取值，而 dto 一旦投影成
// filter，含条件分支的流程在表单页会**静默**退化成只读兜底页（不报错、不 500），
// 作者再也改不了流程。这个回归只有读代码才发现，所以钉一条断言。
func TestAutomationItemProjectsLegacyNodeType(t *testing.T) {
	entity := &mailmodel.MailAutomationEntity{
		ID: 1, Name: "分支流程", TriggerType: mailmodel.TriggerManual,
		Status: mailmodel.AutomationStatusDraft, Version: 1,
		Definition: mailmodel.JSONMap{
			"schema": float64(2), "entry": "n1",
			"nodes": []any{
				map[string]any{"key": "n1", "type": "filter",
					"params": map[string]any{
						"mode": ConditionModeAll, "conditions": []any{"opened"},
					},
					"out": map[string]any{"yes": "n2", "no": "n3"}},
				map[string]any{"key": "n2", "type": "end", "out": map[string]any{}},
				map[string]any{"key": "n3", "type": "end", "out": map[string]any{}},
			},
		},
	}
	item := automationItemOf(entity)
	if len(item.Nodes) != 3 {
		t.Fatalf("节点数 = %d，期望 3", len(item.Nodes))
	}
	if item.Nodes[0].Type != maildto.NodeTypeLegacyBranch {
		t.Fatalf("dto 里 filter 应投影回 branch，实际 %q", item.Nodes[0].Type)
	}
	if item.Nodes[0].Yes != "n2" || item.Nodes[0].No != "n3" {
		t.Fatalf("分支出边投影 = yes %q / no %q，期望 n2 / n3", item.Nodes[0].Yes, item.Nodes[0].No)
	}
}
