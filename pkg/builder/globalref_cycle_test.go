package builder

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
)

func TestGlobalrefCycleCompileReturnsError(t *testing.T) {

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
