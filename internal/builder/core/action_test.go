package core

import (
	"errors"
	json "go_wp/pkg/jsonx"
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
