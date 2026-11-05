package presets

import (
	"context"
	"errors"
	"github.com/CloudyKit/jet/v6"
	"go_wp/internal/templates"
	"go_wp/pkg/builder"
	"reflect"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
	json "go_wp/pkg/jsonx"
)

func commercePresetPage(t *testing.T) *builder.Page {
	t.Helper()
	raw, err := (BusinessPresetRegistry{}).ResolvePreset(context.Background(), "commerce.cart-checkout", 1)
	if err != nil {
		t.Fatal(err)
	}
	page, err := builder.ParsePage(raw)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestBusinessPresetCompilesExplicitActionsAndSharedInstances(t *testing.T) {
	page := commercePresetPage(t)
	before, _ := json.Marshal(page)
	compiled, err := builder.Compile(page, []builder.CompileOption{builder.WithComponentSet(presetComponentSet(t)), builder.WithProjectID("p-1"), builder.WithLanguage("zh-CN"), builder.WithCheckoutCountries(checkoutCountries()), builder.WithCompileMode(builder.CompileModePublish)}...)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`data-action-instance="commerce-checkout"`, `data-action-instance="commerce-cart"`, `data-action-kind="local"`, `data-action-part="result"`} {
		if !strings.Contains(compiled.HTML, expected) {
			t.Errorf("未输出解析动作 %s", expected)
		}
	}
	after, _ := json.Marshal(page)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("编译不能修改作者AST")
	}
	reads := 0
	resolver := blockResolverFunc(func(string) ([]*core.Node, error) { reads++; return page.Root, nil })
	refs := &builder.Page{Settings: page.Settings, Root: []*core.Node{
		{ID: "first", Type: "core.globalref", Props: json.RawMessage(`{"blockId":"flow"}`)},
		{ID: "second", Type: "core.globalref", Props: json.RawMessage(`{"blockId":"flow"}`)},
	}}
	options := []builder.CompileOption{builder.WithComponentSet(presetComponentSet(t)), builder.WithProjectID("p-1"), builder.WithLanguage("zh-CN"), builder.WithCheckoutCountries(checkoutCountries()), builder.WithCompileMode(builder.CompileModePublish), builder.WithBlockResolver(resolver)}
	shared, err := builder.Compile(refs, options...)
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"first", "second"} {
		if !strings.Contains(shared.HTML, `data-action-instance="`+prefix+`-b-commerce-checkout"`) {
			t.Errorf("共享实例动作未隔离: %s", prefix)
		}
	}
	if reads != 1 {
		t.Fatalf("同一次编译预检/展开必须共享同一块快照，读取=%d", reads)
	}
	again, err := builder.Compile(refs, options...)
	if err != nil || again.HTML != shared.HTML {
		t.Fatalf("相同输入必须确定性输出: %v", err)
	}
}

func TestIncompleteActionIsLocalButInvalidCapabilityStillFails(t *testing.T) {
	page := commercePresetPage(t)
	var cart, checkout *core.Node
	var visit func([]*core.Node)
	visit = func(nodes []*core.Node) {
		for _, node := range nodes {
			if node.ID == "commerce-cart" {
				cart = node
			}
			if node.ID == "commerce-checkout" {
				checkout = node
			}
			visit(node.Children)
		}
	}
	visit(page.Root)
	cart.Actions["checkout"] = core.ActionTarget{Kind: core.ActionInstance, NodeID: "missing", PartKey: "checkout"}
	preview, err := builder.Compile(page, []builder.CompileOption{builder.WithComponentSet(presetComponentSet(t)), builder.WithProjectID("p-1"), builder.WithLanguage("zh-CN"), builder.WithCheckoutCountries(checkoutCountries()), builder.WithCompileMode(builder.CompileModePreview)}...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.HTML, `data-sky-id="commerce-cart"`) || !strings.Contains(preview.HTML, "每一件心意") || !strings.Contains(preview.HTML, `data-component-instance="commerce-checkout"`) {
		t.Fatal("未完成动作应仅占位源节点，兄弟保留")
	}
	if _, err := builder.Compile(page, []builder.CompileOption{builder.WithComponentSet(presetComponentSet(t)), builder.WithProjectID("p-1"), builder.WithLanguage("zh-CN"), builder.WithCheckoutCountries(checkoutCountries()), builder.WithCompileMode(builder.CompileModePublish)}...); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("发布必须拒绝缺失目标: %v", err)
	}
	checkout.Actions["success"] = core.ActionTarget{Kind: core.ActionInstance, NodeID: cart.ID, PartKey: "cart"}
	if _, err := builder.Compile(page, []builder.CompileOption{builder.WithComponentSet(presetComponentSet(t)), builder.WithProjectID("p-1"), builder.WithLanguage("zh-CN"), builder.WithCheckoutCountries(checkoutCountries()), builder.WithCompileMode(builder.CompileModePreview)}...); err == nil || errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("前面的未完成动作不能掩盖兄弟能力不匹配: %v", err)
	}
}

func TestClonePageRemapsActionsAcrossRoots(t *testing.T) {
	original := &builder.Page{Root: []*core.Node{
		{ID: "a", Type: "core.cartIcon", Actions: map[string]core.ActionTarget{"checkout": {Kind: core.ActionInstance, NodeID: "b", PartKey: "checkout"}}},
		{ID: "b", Type: "core.checkoutForm", Actions: map[string]core.ActionTarget{"success": {Kind: core.ActionLocal, PartKey: "result"}}},
	}}
	cloned := builder.ClonePageWithNewIDs(original)
	if cloned.Root[0].ID == "a" || cloned.Root[0].Actions["checkout"].NodeID != cloned.Root[1].ID || cloned.Root[1].Actions["success"].PartKey != "result" {
		t.Fatal("整页复制必须保留动作并重映射跨root目标")
	}
	cloned.Root[0].Actions["checkout"] = core.ActionTarget{}
	if original.Root[0].Actions["checkout"].NodeID != "b" {
		t.Fatal("复制动作不能共享map")
	}
}

type blockResolverFunc func(blockID string) ([]*core.Node, error)

func (f blockResolverFunc) ResolveBlockRoot(blockID string) ([]*core.Node, error) { return f(blockID) }

// presetComponentSet 构造带组件模板的 Set。
func presetComponentSet(t *testing.T) *jet.Set {
	t.Helper()
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("构建组件模板集失败: %v", err)
	}
	return set
}
