package templates

// admin_template_yield_test.go — 配平判据对 Jet 两种 yield 形态的识别。
//
// 背景：判据只统计 if/range/block/with 的开头，曾漏掉 yield 的「内容槽」形式
//（{{yield b content}} 必须配 end），导致一次合法的片段抽取被 16 个模板同时误报。
// 这里把两种形态的边界钉住，避免下次再踩。

import "testing"

// 内容槽 yield 配了 end：应当无结构问题。
func TestTemplateProblems_ContentSlotYieldBalanced(t *testing.T) {
	src := "{{block body()}}{{yield item content}}{{end}}{{end}}"
	if got := templateProblems(src); len(got) != 0 {
		t.Fatalf("配平的内容槽 yield 不应报结构问题，got %v", got)
	}
}

// 内容槽 yield 少了 end：必须报出来（这正是判据要守的）。
func TestTemplateProblems_ContentSlotYieldMissingEnd(t *testing.T) {
	src := "{{block body()}}{{yield item content}}{{end}}"
	got := templateProblems(src)
	if len(got) == 0 {
		t.Fatal("内容槽 yield 缺 end 应当报不配平")
	}
}

// 无内容 yield 不需要 end：不能被当成缺 end（否则每个正常调用都误报）。
func TestTemplateProblems_PlainYieldNeedsNoEnd(t *testing.T) {
	src := "{{block body()}}{{yield toolbar()}}{{yield bulkBtn(label=x)}}{{end}}"
	if got := templateProblems(src); len(got) != 0 {
		t.Fatalf("无内容 yield 不应报结构问题，got %v", got)
	}
}

// 末词只是以 content 结尾（mycontent）不算内容槽。
func TestTemplateProblems_WordEndingWithContentIsNotSlot(t *testing.T) {
	src := "{{block body()}}{{yield b mycontent}}{{end}}"
	if got := templateProblems(src); len(got) != 0 {
		t.Fatalf("末词 mycontent 不是内容槽，不应额外计一个 open，got %v", got)
	}
}
