package builder

// globalref 循环引用回归测试（docs/02-F 动效实现中发现的真实 OOM 缺陷）。
//
// 缺陷史：块 a 引用 b、块 b 引用 a 时，渲染路径（globalrefViewOf → 递归展开）
// 无任何防护，无限深拷贝节点树导致内存指数级消耗——机器 OOM，而非栈溢出。
// 修复：globalrefViewOf 维护 ctx.BlockStack 展开栈，同一块 ID 嵌套即报
// 「循环引用」；另加 32 层深度上限。本测试固化该行为：Compile 必须返回
// 明确错误，绝不 OOM。
import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

func TestGlobalrefCycleCompileReturnsError(t *testing.T) {
	// 块 a 引用 b，块 b 引用 a —— 形成环。
	cyclic := map[string]string{
		"a": `{"settings":{},"root":[{"id":"a1","type":"core.text","props":{"text":"环-A"}},{"id":"a2","type":"core.globalref","props":{"blockId":"b"}}]}`,
		"b": `{"settings":{},"root":[{"id":"b1","type":"core.text","props":{"text":"环-B"}},{"id":"b2","type":"core.globalref","props":{"blockId":"a"}}]}`,
	}
	resolve := func(blockID string) ([]*core.Node, error) {
		doc, ok := cyclic[blockID]
		if !ok {
			return nil, errBlockUnavailable
		}
		p, err := ParsePage([]byte(doc))
		if err != nil {
			return nil, err
		}
		return p.Root, nil
	}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"r1","type":"core.globalref","props":{"blockId":"a"}}]}`
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err = Compile(p, WithComponentSet(set), WithBlockResolver(blockResolverFunc(resolve)))
	if err == nil {
		t.Fatal("循环引用必须返回错误（修复前此处无限展开直至 OOM）")
	}
	if !strings.Contains(err.Error(), "循环引用") {
		t.Fatalf("错误应包含「循环引用」，got: %v", err)
	}
}
