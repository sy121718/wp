package builder

import (
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
	"testing"
)

type globalrefUsage map[string]bool

func (u globalrefUsage) UseBlock(id string)   { u[id] = true }
func (u globalrefUsage) UseSiteSlot(string)   {}
func (u globalrefUsage) UseMenu(string)       {}
func (u globalrefUsage) UseNavigation(string) {}

func TestGlobalrefRecordsNestedCompileDependencies(t *testing.T) {
	documents := map[string]string{
		"outer": `{"root":[{"id":"inner-ref","type":"core.globalref","props":{"blockId":"inner"}}]}`,
		"inner": `{"root":[{"id":"text","type":"core.text","props":{"text":"共享内容"}}]}`,
	}
	resolver := blockResolverFunc(func(id string) ([]*core.Node, error) {
		doc, err := ParsePage([]byte(documents[id]))
		if err != nil {
			return nil, err
		}
		return doc.Root, nil
	})
	doc, err := ParsePage([]byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"ref","type":"core.globalref","props":{"blockId":"outer"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	usage := globalrefUsage{}
	if _, err := Compile(doc, WithComponentSet(set), WithBlockResolver(resolver), WithUsageRecorder(usage)); err != nil {
		t.Fatal(err)
	}
	if !usage["outer"] || !usage["inner"] {
		t.Fatalf("嵌套全局块必须全部进入构建依赖：%v", usage)
	}
}
