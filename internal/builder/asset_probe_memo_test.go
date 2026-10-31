package builder

import (
	"fmt"
	"strings"
	"testing"

	mediacontract "go_wp/internal/module/media/contract"
	"go_wp/internal/templates"
)

type countingGenProbe struct {
	counts map[string]int
	gen    int
}

func newCountingGenProbe(gen int) *countingGenProbe {
	return &countingGenProbe{counts: make(map[string]int), gen: gen}
}

func (p *countingGenProbe) probe(url string) []mediacontract.VariantRef {
	p.counts[url]++
	if url != "https://media.example.test/storage/a.jpg" {
		return nil
	}
	return []mediacontract.VariantRef{
		{URL: fmt.Sprintf("https://media.example.test/storage/a_thumb-%d-ab12cd34.jpg", p.gen), Width: 320},
		{URL: fmt.Sprintf("https://media.example.test/storage/a_medium-%d-ab12cd34.jpg", p.gen), Width: 1280},
	}
}

func (p *countingGenProbe) total() int {
	n := 0
	for _, c := range p.counts {
		n += c
	}
	return n
}

const probeMemoDoc = `{"settings":{"layout":{"mode":"full"}},"root":[
	{"id":"i1","type":"core.image","props":{"src":"https://media.example.test/storage/a.jpg","alt":"A"}},
	{"id":"i2","type":"core.image","props":{"src":"https://media.example.test/storage/a.jpg","alt":"B"}},
	{"id":"i3","type":"core.image","props":{"src":"https://media.example.test/storage/a.jpg","alt":"C"}},
	{"id":"i4","type":"core.image","props":{"src":"https://media.example.test/storage/b.jpg","alt":"D"}}
]}`

func TestAssetProbeMemoWithinCompile(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	probe := newCountingGenProbe(1)
	page, err := ParsePage([]byte(probeMemoDoc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page,
		WithComponentSet(set),
		WithThemeSettings(&ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}),
		WithAssetProbe(probe.probe))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	if got := probe.counts["https://media.example.test/storage/a.jpg"]; got != 1 {
		t.Errorf("同一 URL 在一次编译内应只探测 1 次，实际 %d 次", got)
	}
	if got := probe.counts["https://media.example.test/storage/b.jpg"]; got != 1 {
		t.Errorf("空结果也应走同一条去重路径（应 1 次），实际 %d 次", got)
	}
	if got := probe.total(); got != 2 {
		t.Errorf("本次编译探测总次数应为 2，实际 %d", got)
	}
	t.Logf("探测次数明细: a.jpg=%d b.jpg=%d 合计=%d（文档里 a.jpg 出现 3 次）",
		probe.counts["https://media.example.test/storage/a.jpg"], probe.counts["https://media.example.test/storage/b.jpg"], probe.total())

	if got := strings.Count(c.HTML, "https://media.example.test/storage/a_medium-1-ab12cd34.jpg 1280w"); got != 3 {
		t.Errorf("三个同 src 节点都应输出 srcset 候选，实际 %d 个\n%s", got, c.HTML)
	}
}

func TestAssetProbeMemoDoesNotOutliveCompile(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	probe := newCountingGenProbe(1)
	page, err := ParsePage([]byte(probeMemoDoc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	opts := []CompileOption{
		WithComponentSet(set),
		WithThemeSettings(&ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}),
		WithAssetProbe(probe.probe),
	}

	first, err := Compile(page, opts...)
	if err != nil {
		t.Fatalf("第一次编译失败: %v", err)
	}
	if !strings.Contains(first.HTML, "https://media.example.test/storage/a_thumb-1-ab12cd34.jpg") {
		t.Fatalf("第一次编译缺 generation=1 的变体\n%s", first.HTML)
	}
	firstCalls := probe.total()

	probe.gen = 2
	second, err := Compile(page, opts...)
	if err != nil {
		t.Fatalf("第二次编译失败: %v", err)
	}
	if strings.Contains(second.HTML, "a_thumb-1-ab12cd34.jpg") || strings.Contains(second.HTML, "a_medium-1-ab12cd34.jpg") {
		t.Errorf("第二次编译仍输出旧 generation 的变体 URL —— 缓存跨构建陈旧了\n%s", second.HTML)
	}
	if !strings.Contains(second.HTML, "https://media.example.test/storage/a_thumb-2-ab12cd34.jpg") {
		t.Errorf("第二次编译缺新 generation 的变体，新图在产物里不可见\n%s", second.HTML)
	}

	if secondCalls := probe.total(); secondCalls-firstCalls != 2 {
		t.Errorf("第二次编译应重新探测 2 次（不继承上一次的缓存），实际增量 %d", secondCalls-firstCalls)
	}
	t.Logf("两次编译探测计数: 第一次后合计=%d，第二次后合计=%d（每次编译各自 2 次）", firstCalls, probe.total())
}
