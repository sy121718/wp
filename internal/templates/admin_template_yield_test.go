package templates

import "testing"

func TestTemplateProblems_ContentSlotYieldBalanced(t *testing.T) {
	src := "{{block body()}}{{yield item content}}{{end}}{{end}}"
	if got := templateProblems(src); len(got) != 0 {
		t.Fatalf("配平的内容槽 yield 不应报结构问题，got %v", got)
	}
}

func TestTemplateProblems_ContentSlotYieldMissingEnd(t *testing.T) {
	src := "{{block body()}}{{yield item content}}{{end}}"
	got := templateProblems(src)
	if len(got) == 0 {
		t.Fatal("内容槽 yield 缺 end 应当报不配平")
	}
}

func TestTemplateProblems_PlainYieldNeedsNoEnd(t *testing.T) {
	src := "{{block body()}}{{yield toolbar()}}{{yield bulkBtn(label=x)}}{{end}}"
	if got := templateProblems(src); len(got) != 0 {
		t.Fatalf("无内容 yield 不应报结构问题，got %v", got)
	}
}

func TestTemplateProblems_WordEndingWithContentIsNotSlot(t *testing.T) {
	src := "{{block body()}}{{yield b mycontent}}{{end}}"
	if got := templateProblems(src); len(got) != 0 {
		t.Fatalf("末词 mycontent 不是内容槽，不应额外计一个 open，got %v", got)
	}
}
