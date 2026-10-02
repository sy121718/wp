package builder

// asset_probe_memo_test.go —— 「图片变体探测按编译作用域 memoize」的证据测试。
//
// 两条判据：
//  1. 一次编译里同一 URL 只探测一次（同 src 的多个节点共享一次探测）；
//  2. 缓存不跨构建 —— 两次连续 Compile 各自重新探测，变体 generation 变了之后
//     第二次编译必须给出**新** URL（若缓存挂在装配期那个跨构建的闭包上，这条会红）。

import (
	"fmt"
	"strings"
	"testing"

	mediacontract "go_wp/internal/module/media/contract"
	"go_wp/internal/templates"
)

// countingGenProbe 假探测函数：统计调用次数，并按当前 generation 拼变体名
// （与 media 模块真实命名同形：<stem>_<type>-<generation>-<hash8>.jpg）。
//
// 测试单协程运行，不加锁；计数与 generation 都直接读写。
type countingGenProbe struct {
	counts map[string]int
	gen    int
}

func newCountingGenProbe(gen int) *countingGenProbe {
	return &countingGenProbe{counts: make(map[string]int), gen: gen}
}

func (p *countingGenProbe) probe(url string) []mediacontract.VariantRef {
	p.counts[url]++
	if url != "/storage/a.jpg" {
		return nil // 非媒体库 URL / 无变体：空结果也必须被缓存
	}
	return []mediacontract.VariantRef{
		{URL: fmt.Sprintf("/storage/a_thumb-%d-ab12cd34.jpg", p.gen), Width: 320},
		{URL: fmt.Sprintf("/storage/a_medium-%d-ab12cd34.jpg", p.gen), Width: 1280},
	}
}

func (p *countingGenProbe) total() int {
	n := 0
	for _, c := range p.counts {
		n += c
	}
	return n
}

// probeMemoDoc 同一张图出现 3 次（3 个 image 节点同 src）+ 另一张无变体的图 1 次。
const probeMemoDoc = `{"settings":{"layout":{"mode":"full"}},"root":[
	{"id":"i1","type":"core.image","props":{"src":"/storage/a.jpg","alt":"A"}},
	{"id":"i2","type":"core.image","props":{"src":"/storage/a.jpg","alt":"B"}},
	{"id":"i3","type":"core.image","props":{"src":"/storage/a.jpg","alt":"C"}},
	{"id":"i4","type":"core.image","props":{"src":"/storage/b.jpg","alt":"D"}}
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

	// ① 次数：a.jpg 在文档里出现 3 次、b.jpg 出现 1 次 → 去重后总探测 2 次。
	if got := probe.counts["/storage/a.jpg"]; got != 1 {
		t.Errorf("同一 URL 在一次编译内应只探测 1 次，实际 %d 次", got)
	}
	if got := probe.counts["/storage/b.jpg"]; got != 1 {
		t.Errorf("空结果也应走同一条去重路径（应 1 次），实际 %d 次", got)
	}
	if got := probe.total(); got != 2 {
		t.Errorf("本次编译探测总次数应为 2，实际 %d", got)
	}
	t.Logf("探测次数明细: a.jpg=%d b.jpg=%d 合计=%d（文档里 a.jpg 出现 3 次）",
		probe.counts["/storage/a.jpg"], probe.counts["/storage/b.jpg"], probe.total())

	// ② 命中缓存仍照常产出：三个 a.jpg 节点都应有 srcset（缓存值被消费，不是被吞掉）。
	if got := strings.Count(c.HTML, "/storage/a_medium-1-ab12cd34.jpg 1280w"); got != 3 {
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
	// 复用同一份 opts（同一个注入闭包）两次编译 —— 模拟装配层复用同一探测函数。
	opts := []CompileOption{
		WithComponentSet(set),
		WithThemeSettings(&ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}),
		WithAssetProbe(probe.probe),
	}

	first, err := Compile(page, opts...)
	if err != nil {
		t.Fatalf("第一次编译失败: %v", err)
	}
	if !strings.Contains(first.HTML, "/storage/a_thumb-1-ab12cd34.jpg") {
		t.Fatalf("第一次编译缺 generation=1 的变体\n%s", first.HTML)
	}
	firstCalls := probe.total()

	// 换图 / 重新生成变体：generation 变了（本仓库刚修过的同源正确性问题）。
	probe.gen = 2
	second, err := Compile(page, opts...)
	if err != nil {
		t.Fatalf("第二次编译失败: %v", err)
	}
	if strings.Contains(second.HTML, "a_thumb-1-ab12cd34.jpg") || strings.Contains(second.HTML, "a_medium-1-ab12cd34.jpg") {
		t.Errorf("第二次编译仍输出旧 generation 的变体 URL —— 缓存跨构建陈旧了\n%s", second.HTML)
	}
	if !strings.Contains(second.HTML, "/storage/a_thumb-2-ab12cd34.jpg") {
		t.Errorf("第二次编译缺新 generation 的变体，新图在产物里不可见\n%s", second.HTML)
	}

	// 两次编译各自重新探测：第二次的增量必须等于「一次编译的去重后次数」。
	if secondCalls := probe.total(); secondCalls-firstCalls != 2 {
		t.Errorf("第二次编译应重新探测 2 次（不继承上一次的缓存），实际增量 %d", secondCalls-firstCalls)
	}
	t.Logf("两次编译探测计数: 第一次后合计=%d，第二次后合计=%d（每次编译各自 2 次）", firstCalls, probe.total())
}
