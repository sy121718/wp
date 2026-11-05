package core

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"reflect"
	"testing"
)

func TestActionTargetClosedShape(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"page","pageId":"p","partKey":"cart"}`,
		`{"kind":"local","partKey":"cart","newTab":false}`,
		`{"kind":"instance","nodeId":"cart-node","partKey":"cart","selector":"#cart"}`,
	} {
		var target ActionTarget
		if err := json.Unmarshal([]byte(raw), &target); err == nil {
			t.Fatalf("跨类型/未知字段必须拒绝: %s", raw)
		}
	}
	for _, target := range []ActionTarget{
		{Kind: ActionPage, PageID: "p"}, {Kind: ActionPresentation, InstanceID: "i"},
		{Kind: ActionLocal, PartKey: "checkout"}, {Kind: ActionInstance, NodeID: "cart-node", PartKey: "cart"},
		{Kind: ActionExternal, URL: "https://example.test/docs", NewTab: true},
	} {
		if err := ValidateActionTarget(target); err != nil {
			t.Fatalf("合法目标被拒绝: %+v %v", target, err)
		}
	}
	for _, target := range []ActionTarget{{Kind: "script"}, {Kind: ActionLocal, PartKey: "#cart"}, {Kind: ActionExternal, URL: "javascript:alert(1)"}, {Kind: ActionExternal, URL: "/docs"}, {Kind: ActionExternal, URL: "https://user:secret@example.test/"}} {
		if err := ValidateActionTarget(target); err == nil {
			t.Fatalf("非法目标未拒绝: %+v", target)
		}
	}
	if err := ValidateActionTarget(ActionTarget{Kind: ActionInstance, NodeID: "cart-node"}); !errors.Is(err, ErrIncompleteNode) {
		t.Fatalf("缺part应为未完成: %v", err)
	}
}
func TestActionResolverIsExplicitAndNodeActionsRoundTrip(t *testing.T) {
	var node Node
	raw := []byte(`{"id":"source","type":"core.button","props":{},"actions":{"open":{"kind":"instance","nodeId":"cart","partKey":"cart"}}}`)
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatal(err)
	}
	if node.Actions["open"].NodeID != "cart" {
		t.Fatal("动作未保存在节点结构层")
	}
	ctx := &RenderContext{}
	if _, err := ctx.ResolveAction(&node, "open"); !errors.Is(err, ErrIncompleteNode) {
		t.Fatalf("未解析时不得猜路径: %v", err)
	}
	ctx.SetActionResolver(func(got *Node, key string) (ResolvedAction, error) {
		if got != &node || key != "open" {
			t.Fatal("解析上下文丢失")
		}
		return ResolvedAction{Kind: ActionInstance, InstanceID: "cart", PartKey: "cart"}, nil
	})
	got, err := ctx.ResolveAction(&node, "open")
	if err != nil || got.InstanceID != "cart" {
		t.Fatalf("受控解析结果: %+v %v", got, err)
	}
}

type actionTestComponent struct{}

func (actionTestComponent) Type() string                          { return "test.actions" }
func (actionTestComponent) Validate(*Node, map[string]bool) error { return nil }
func (actionTestComponent) Actions() []ActionSpec {
	return []ActionSpec{{Key: "open", AllowedKinds: []ActionKind{ActionInstance}}, {Key: "other", AllowedKinds: []ActionKind{ActionLocal}}}
}
func (actionTestComponent) RequiredActions() []string { return []string{"open"} }

func TestActionDeclarationsRejectInvalidAfterIncomplete(t *testing.T) {
	component := actionTestComponent{}
	node := &Node{ID: "source"}
	if err := ValidateNodeActions(node, component); !errors.Is(err, ErrIncompleteNode) {
		t.Fatalf("缺必需动作应可保存草稿: %v", err)
	}
	node.Actions = map[string]ActionTarget{"open": {Kind: ActionInstance}, "other": {Kind: ActionExternal, URL: "https://example.test"}}
	if err := ValidateNodeActions(node, component); err == nil || errors.Is(err, ErrIncompleteNode) {
		t.Fatalf("未完成不能吞掉后续非法目标种类: %v", err)
	}
	node.Actions = map[string]ActionTarget{"undeclared": {Kind: ActionLocal, PartKey: "result"}}
	if err := ValidateNodeActions(node, component); err == nil {
		t.Fatal("组件未声明动作必须拒绝")
	}
}

func TestActionTargetJSONXRoundTripAllKinds(t *testing.T) {
	for _, target := range []ActionTarget{
		{Kind: ActionPage, PageID: "page"},
		{Kind: ActionPresentation, InstanceID: "presentation"},
		{Kind: ActionLocal, PartKey: "result"},
		{Kind: ActionInstance, NodeID: "checkout", PartKey: "checkout"},
		{Kind: ActionExternal, URL: "https://example.test/help"},
		{Kind: ActionExternal, URL: "https://example.test/help", NewTab: true},
	} {
		raw, err := json.Marshal(target)
		if err != nil {
			t.Fatal(err)
		}
		var got ActionTarget
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("合法目标序列化后不可读取: %s: %v", raw, err)
		}
		if !reflect.DeepEqual(got, target) {
			t.Fatalf("动作往返改变: got=%+v want=%+v", got, target)
		}
	}
}
