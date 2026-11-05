package pipeline

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
)

func TestActionTargetDependenciesComeFromSuccessfulComponentConsumption(t *testing.T) {
	doc := []byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"one","type":"core.cartIcon","props":{"mode":"inline"},"actions":{"checkout":{"kind":"page","pageId":"chosen-page"}}},{"id":"two","type":"core.cartIcon","props":{"mode":"inline"},"actions":{"checkout":{"kind":"page","pageId":"chosen-page"}}},{"id":"three","type":"core.cartIcon","props":{"mode":"inline"},"actions":{"checkout":{"kind":"presentation","instanceId":"chosen-instance"}}}]}`)
	page, err := builder.ParsePage(doc)
	if err != nil {
		t.Fatal(err)
	}
	set, _, err := ComponentSetWithPlugins(nil)
	if err != nil {
		t.Fatal(err)
	}
	usage := &CompileUsage{}
	resolved := func(node *core.Node, key string) (core.ResolvedAction, error) {
		target := node.Actions[key]
		return core.ResolvedAction{Kind: target.Kind, URL: "/published-" + string(target.Kind)}, nil
	}
	compiled, err := builder.Compile(page, builder.WithProjectID("p"), builder.WithComponentSet(set), builder.WithUsageRecorder(usage), builder.WithActionResolver(resolved), builder.WithCompileMode(builder.CompileModePublish))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(compiled.HTML, `href="/published-page"`) != 2 || !strings.Contains(compiled.HTML, `href="/published-presentation"`) {
		t.Fatal("动作必须确实被组件输出")
	}
	expected := []Dependency{{Kind: DepKindPageTarget, Key: "page_target:chosen-page"}, {Kind: DepKindPresentationTarget, Key: "presentation_target:chosen-instance"}}
	if got := ActionTargetDependencies(usage); !reflect.DeepEqual(got, expected) {
		t.Fatalf("实际消费目标与依赖必须等式: got=%+v want=%+v", got, expected)
	}

	ctx := &core.RenderContext{}
	empty := &CompileUsage{}
	ctx.SetUsageRecorder(empty)
	ctx.SetActionResolver(func(*core.Node, string) (core.ResolvedAction, error) {
		return core.ResolvedAction{}, fmt.Errorf("未发布")
	})
	if _, err = ctx.ResolveAction(page.Root[0], "checkout"); err == nil {
		t.Fatal("失败解析不得成功")
	}
	ctx.UseActionTarget(core.ActionExternal, "outside")
	ctx.UseActionTarget(core.ActionLocal, "part")
	ctx.UseActionTarget(core.ActionInstance, "same-page-node")
	if len(ActionTargetDependencies(empty)) != 0 {
		t.Fatal("失败/本地/外链不能制造跨页依赖")
	}
}
