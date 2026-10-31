package mailservice

import (
	"strings"
	"testing"
)

func node(key, typ string, params map[string]any, next string) map[string]any {
	n := map[string]any{"key": key, "type": typ}
	if params != nil {
		n["params"] = params
	}
	if next != "" {
		n["next"] = next
	}
	return n
}

func branch(key string, conditions []any, yes, no string) map[string]any {
	return map[string]any{
		"key": key, "type": NodeTypeBranch,
		"params": map[string]any{"conditions": conditions},
		"yes":    yes, "no": no,
	}
}

func parseFromNodes(t *testing.T, entry string, nodes []map[string]any) error {
	t.Helper()
	list := make([]any, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	_, err := ParseDefinition(map[string]any{"entry": entry, "nodes": list})
	return err
}

func TestValidGraphPasses(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeTrigger, nil, "n2"),
		node("n2", NodeTypeDelay, map[string]any{"minutes": 60}, "n3"),
		node("n3", NodeTypeEmail, map[string]any{"template_key": "welcome"}, "n4"),
		node("n4", NodeTypeTag, map[string]any{"add": []any{"hot"}}, "n5"),
		node("n5", NodeTypeEnd, nil, ""),
	})
	if err != nil {
		t.Fatalf("合法图不该报错: %v", err)
	}
}

func TestBranchWithTwoArmsPasses(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeTrigger, nil, "n2"),
		branch("n2", []any{"opened"}, "n3", "n4"),
		node("n3", NodeTypeTag, map[string]any{"add": []any{"engaged"}}, "n5"),
		node("n4", NodeTypeTag, map[string]any{"add": []any{"cold"}}, "n5"),
		node("n5", NodeTypeEnd, nil, ""),
	})
	if err != nil {
		t.Fatalf("两条分支汇合的图是合法的: %v", err)
	}
}

func TestDelayMinutesAsFloat(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeDelay, map[string]any{"minutes": float64(60)}, "n2"),
		node("n2", NodeTypeEnd, nil, ""),
	})
	if err != nil {
		t.Fatalf("float64 的 minutes 应被接受: %v", err)
	}
}

func TestDuplicateKeyRejected(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeTrigger, nil, "n2"),
		node("n2", NodeTypeEnd, nil, ""),
		node("n2", NodeTypeEnd, nil, ""),
	})
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("应该报 key 重复，实际 %v", err)
	}
}

func TestMissingEntryRejected(t *testing.T) {
	if err := parseFromNodes(t, "nope", []map[string]any{node("n1", NodeTypeEnd, nil, "")}); err == nil {
		t.Fatal("入口不存在应被拒绝")
	}
	if err := parseFromNodes(t, "", []map[string]any{node("n1", NodeTypeEnd, nil, "")}); err == nil {
		t.Fatal("没有入口应被拒绝")
	}
}

func TestDanglingEdgeRejected(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeTrigger, nil, "ghost"),
	})
	if err == nil || !strings.Contains(err.Error(), "不存在的节点") {
		t.Fatalf("指向不存在的节点应被拒绝，实际 %v", err)
	}
}

func TestCycleRejected(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeTrigger, nil, "n2"),
		node("n2", NodeTypeDelay, map[string]any{"minutes": 1}, "n3"),
		node("n3", NodeTypeEmail, map[string]any{"template_key": "welcome"}, "n2"),
	})
	if err == nil || !strings.Contains(err.Error(), "环") {
		t.Fatalf("有环应被拒绝，实际 %v", err)
	}
}

func TestSelfLoopRejected(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeTrigger, nil, "n1"),
	})
	if err == nil || !strings.Contains(err.Error(), "环") {
		t.Fatalf("自环应被拒绝，实际 %v", err)
	}
}

func TestUnreachableNodeRejected(t *testing.T) {
	err := parseFromNodes(t, "n1", []map[string]any{
		node("n1", NodeTypeEnd, nil, ""),
		node("orphan", NodeTypeEmail, map[string]any{"template_key": "welcome"}, ""),
	})
	if err == nil || !strings.Contains(err.Error(), "走不到") {
		t.Fatalf("不可达节点应被拒绝，实际 %v", err)
	}
}

func TestNodeShapeValidation(t *testing.T) {
	cases := []struct {
		name  string
		nodes []map[string]any
		want  string
	}{
		{"分支缺一条出边", []map[string]any{
			{"key": "n1", "type": NodeTypeBranch, "params": map[string]any{"conditions": []any{"opened"}}, "yes": "n2"},
			node("n2", NodeTypeEnd, nil, ""),
		}, "两条出边"},
		{"分支没有条件", []map[string]any{
			branch("n1", []any{}, "n2", "n2"),
			node("n2", NodeTypeEnd, nil, ""),
		}, "至少一个条件"},
		{"等待缺 minutes", []map[string]any{
			node("n1", NodeTypeDelay, map[string]any{}, "n2"),
			node("n2", NodeTypeEnd, nil, ""),
		}, "minutes"},
		{"等待 minutes 为负", []map[string]any{
			node("n1", NodeTypeDelay, map[string]any{"minutes": -5}, "n2"),
			node("n2", NodeTypeEnd, nil, ""),
		}, "minutes"},
		{"发信缺模板", []map[string]any{
			node("n1", NodeTypeEmail, map[string]any{}, "n2"),
			node("n2", NodeTypeEnd, nil, ""),
		}, "template_key"},
		{"标签节点什么都没配", []map[string]any{
			node("n1", NodeTypeTag, map[string]any{}, "n2"),
			node("n2", NodeTypeEnd, nil, ""),
		}, "add 或 remove"},
		{"未知类型", []map[string]any{
			{"key": "n1", "type": "teleport"},
		}, "未知节点类型"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := parseFromNodes(t, "n1", c.nodes)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("应报含 %q 的错误，实际 %v", c.want, err)
			}
		})
	}
}
