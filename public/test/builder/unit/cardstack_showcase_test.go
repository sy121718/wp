package unit

// cardstack_showcase_test.go — core.cardstack 全能力实例页的回归测试。
//
// 素材：public/test/fixtures/cardstack/showcase.json（15 个区块，覆盖五种交互模式、
// 三种内容来源、卡片级样式、切换动画与循环高亮）。
//
// 为什么要有这个测试：单组件测试只看得到局部，模板截断、盒模型偏移、动画属性冲突
// 这类缺陷只有在「全部模式铺在一个长页面里」时才暴露（本会话两个真 bug 都是这么发现的）。
// 这个用例把整页编译一遍并断言每种模式的关键特征，产物同时写到 /tmp 供人工查看。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

// showcaseCollection 假内容解析器：商品 / 文章两类内容，category 故意为空（演示空状态）。
type showcaseCollection struct{}

func (showcaseCollection) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	switch source {
	case "content:product":
		return []map[string]any{
			{"id": 1, "slug": "graphite", "name": "石墨黑 · 标准版", "price": "¥ 299", "description": "磨砂铝合金外壳。", "images": []any{"https://placehold.co/480x280/1f2430/ffffff?text=Graphite"}},
			{"id": 2, "slug": "fog", "name": "云雾白 · 标准版", "price": "¥ 299", "description": "阳极氧化白。", "images": []any{"https://placehold.co/480x280/c9d3e0/1f2430?text=Fog"}},
			{"id": 3, "slug": "emerald", "name": "松石绿 · 限定版", "price": "¥ 359", "description": "限定配色。", "images": []any{"https://placehold.co/480x280/00b894/ffffff?text=Emerald"}},
			{"id": 4, "slug": "peach", "name": "蜜桃粉 · 限定版", "price": "¥ 359", "description": "哑光喷涂。", "images": []any{"https://placehold.co/480x280/ff8fa3/3a2a20?text=Peach"}},
		}, nil
	case "content:article":
		return []map[string]any{
			{"id": 5, "slug": "static-pipeline", "title": "静态发布管线是怎么跑起来的", "excerpt": "控制面编译 → 不可变 Artifact。", "featuredImage": "https://placehold.co/480x260/5e5cfc/ffffff?text=Pipeline"},
			{"id": 6, "slug": "deterministic-css", "title": "为什么把组件几何写进静态 CSS", "excerpt": "同输入同字节。", "featuredImage": "https://placehold.co/480x260/00b894/ffffff?text=Determinism"},
			{"id": 7, "slug": "zero-js", "title": "零 JS 交互的三条路径", "excerpt": ":target / :checked / scroll-driven。", "featuredImage": "https://placehold.co/480x260/f0a37f/3a2a20?text=Zero+JS"},
		}, nil
	}
	return nil, nil // category 故意为空
}

func (showcaseCollection) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{
		{Source: "content:product", Label: "商品列表", Fields: []string{"name", "description", "price", "images"}},
		{Source: "content:article", Label: "文章列表", Fields: []string{"title", "excerpt", "featuredImage"}},
		{Source: "content:category", Label: "分类列表", Fields: []string{"name", "description", "image"}},
	}, nil
}

// TestCardstackShowcase 编译整页实例并逐种模式断言关键特征。
func TestCardstackShowcase(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "cardstack", "showcase.json"))
	if err != nil {
		t.Fatalf("读实例 fixture 失败: %v", err)
	}
	p, err := builder.ParsePage(raw)
	if err != nil {
		t.Fatalf("解析实例文档失败（fixture 与文档模型漂移了）: %v", err)
	}
	c, err := compile(t, p, builder.WithCollectionResolver(showcaseCollection{}))
	if err != nil {
		t.Fatalf("编译实例页失败: %v", err)
	}
	html, css := c.HTML, c.CSS

	// 结构完整性：标签必须配平 —— 模板丢闭合标签时这里会直接红。
	for _, pair := range []struct{ open, close, name string }{
		{"<div", "</div>", "div"},
		{"<label", "</label>", "label"},
	} {
		if o, cl := strings.Count(html, pair.open), strings.Count(html, pair.close); o != cl {
			t.Errorf("%s 标签不配平：开 %d / 闭 %d", pair.name, o, cl)
		}
	}

	// 盒子模型：有几何计算的组件必须 border-box，否则参数与实际外尺寸不符。
	if !strings.Contains(css, "box-sizing: border-box") {
		t.Errorf("卡片缺少 box-sizing: border-box")
	}
	// 扇形：rotate 在 translate 之前（弧线）+ 色相派生
	if !strings.Contains(css, "rotate(-20deg) translate(-") || !strings.Contains(css, "hue-rotate(") {
		t.Errorf("扇形模式缺少弧线变换或色相派生")
	}
	// 悬停发光：只走 filter，不抢位移的 transform
	if !strings.Contains(css, "animation: sky-loop-glow 2s ease-in-out infinite") {
		t.Errorf("悬停发光缺失")
	}
	// 竖排：纯纵向平移
	if !strings.Contains(css, "translate(0px, -") {
		t.Errorf("竖排模式缺少纵向平移")
	}
	// 滚动堆叠：sticky + view() 时间线
	if !strings.Contains(css, "position: sticky") || !strings.Contains(css, "animation-timeline: view()") {
		t.Errorf("滚动堆叠缺失 sticky 或 view() 时间线")
	}
	// 拖拽旋转：环形几何 + 旋转变量
	if !strings.Contains(css, "--sky-cardstack-rot") {
		t.Errorf("拖拽旋转缺旋转变量")
	}
	// deck：偏移变量 + 回弹曲线
	if !strings.Contains(css, "--sky-deck-off") || !strings.Contains(css, "cubic-bezier(.34,1.56,.64,1)") {
		t.Errorf("堆叠轮播缺少偏移变量或回弹曲线")
	}
	// deck 纵向：轴向标记
	if !strings.Contains(html, "data-cardstack-deck-axis=") {
		t.Errorf("纵向 deck 缺少 data-cardstack-deck-axis")
	}
	// 全屏分页：纵横两向 + 隐藏滚动条 + 并列动画
	for _, want := range []string{
		"scroll-snap-type: y mandatory", "scroll-snap-type: x mandatory",
		"scrollbar-width: none", "::-webkit-scrollbar",
		"animation: sky-flip-in-x linear both, sky-loop-glow 2s ease-in-out infinite",
		"animation-range: entry 0% entry 70%, cover 25% cover 75%",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("全屏分页缺少 %q", want)
		}
	}
	// 两种集合来源
	if !strings.Contains(html, "/article/static-pipeline") {
		t.Errorf("字段映射模式缺少「前缀 + slug」拼出的详情链接")
	}
	if !strings.Contains(html, "石墨黑 · 标准版") || !strings.Contains(html, "¥ 299") {
		t.Errorf("子节点模板模式缺少 item.* 解析出的字段值")
	}
	// 空状态
	if !strings.Contains(html, "这个分类下还没有内容") {
		t.Errorf("空状态占位文案缺失")
	}

	// 确定性：同一 fixture 两次编译字节一致
	again, err := compile(t, p, builder.WithCollectionResolver(showcaseCollection{}))
	if err != nil {
		t.Fatalf("二次编译失败: %v", err)
	}
	if again.HTML != html || again.CSS != css {
		t.Errorf("实例页产物不确定：两次编译字节不一致")
	}

	// 产物落盘供人工查看
	doc, err := builder.RenderDocument(c)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	out := filepath.Join(os.TempDir(), "cardstack-showcase.html")
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	t.Logf("实例页已生成：%s（%d 字节）", out, len(doc))
}
