package unit

// cardstack_gallery_test.go — 作品展示页（给外部看的成品页）的生成与回归。
//
// 素材：public/test/fixtures/cardstack/gallery.json —— 按真实用途组织的六个区块，
// 与 showcase.json（能力覆盖/验证用）分工不同：那份是"每种模式都出现"，
// 这份是"每种模式都用在一个像样的场景里"。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

// galleryCollection 展示页用的假内容（商品 4 条 / 文章 3 条）。
type galleryCollection struct{}

func (galleryCollection) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	switch source {
	case "content:product":
		return []map[string]any{
			{"id": 1, "slug": "graphite", "name": "石墨黑 · 标准版", "price": "¥ 299", "description": "磨砂铝合金外壳，标配 65W 快充。", "images": []any{"https://placehold.co/520x300/1f2430/ffffff?text=Graphite"}},
			{"id": 2, "slug": "fog", "name": "云雾白 · 标准版", "price": "¥ 299", "description": "阳极氧化白，久用不易留指纹。", "images": []any{"https://placehold.co/520x300/c9d3e0/1f2430?text=Fog+White"}},
			{"id": 3, "slug": "emerald", "name": "松石绿 · 限定版", "price": "¥ 359", "description": "限定配色，附赠同色收纳袋。", "images": []any{"https://placehold.co/520x300/00b894/ffffff?text=Emerald"}},
			{"id": 4, "slug": "peach", "name": "蜜桃粉 · 限定版", "price": "¥ 359", "description": "哑光喷涂，只此一批。", "images": []any{"https://placehold.co/520x300/ff8fa3/3a2a20?text=Peach"}},
		}, nil
	case "content:article":
		return []map[string]any{
			{"id": 5, "slug": "static-pipeline", "title": "静态发布管线是怎么跑起来的", "excerpt": "控制面编译 → 不可变 Artifact → 访问面只读，访客请求零查库。", "featuredImage": "https://placehold.co/560x300/4f46e5/ffffff?text=Pipeline"},
			{"id": 6, "slug": "deterministic-css", "title": "为什么把组件几何写进静态 CSS", "excerpt": "同输入同字节，产物可缓存可回滚，不依赖运行时测量。", "featuredImage": "https://placehold.co/560x300/00b894/ffffff?text=Determinism"},
			{"id": 7, "slug": "zero-js", "title": "零 JS 交互的三条路径", "excerpt": ":target / :checked / sticky+scroll-driven，各有适用边界。", "featuredImage": "https://placehold.co/560x300/f0a37f/3a2a20?text=Zero+JS"},
		}, nil
	}
	return nil, nil
}

func (galleryCollection) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{
		{Source: "content:product", Label: "商品列表", Fields: []string{"name", "description", "price", "images"}},
		{Source: "content:article", Label: "文章列表", Fields: []string{"title", "excerpt", "featuredImage"}},
	}, nil
}

// TestCardstackGallery 生成展示页并做结构回归。
// 产物写到 outDir（默认 /tmp，便于直接打开看）。
func TestCardstackGallery(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "cardstack", "gallery.json"))
	if err != nil {
		t.Fatalf("读展示页 fixture 失败: %v", err)
	}
	p, err := builder.ParsePage(raw)
	if err != nil {
		t.Fatalf("解析展示页文档失败: %v", err)
	}
	c, err := compile(t, p, builder.WithCollectionResolver(galleryCollection{}))
	if err != nil {
		t.Fatalf("编译展示页失败: %v", err)
	}
	html := c.HTML

	for _, pair := range []struct{ open, close, name string }{
		{"<div", "</div>", "div"},
		{"<label", "</label>", "label"},
	} {
		if o, cl := strings.Count(html, pair.open), strings.Count(html, pair.close); o != cl {
			t.Errorf("%s 标签不配平：开 %d / 闭 %d", pair.name, o, cl)
		}
	}
	// 六个区块的形态特征都要在
	for _, want := range []struct{ needle, desc string }{
		{"rotate(-", "扇形展开的弧线变换"},
		{"--sky-deck-off", "堆叠轮播的偏移变量"},
		{"data-cardstack-deck-axis", "纵向切换的轴向标记"},
		{"scroll-snap-type: y mandatory", "全屏分页的纵向吸附"},
		{"sky-cardstack-page", "全屏分页的页码角标"},
		{"is-enter", "入场动画的类规则"},
		{"data-cardstack-drag", "环形拖拽的定位属性"},
		{"/article/static-pipeline", "字段映射拼出的详情链接"},
		{"石墨黑 · 标准版", "子节点模板解析出的字段值"},
	} {
		if !strings.Contains(html, want.needle) && !strings.Contains(c.CSS, want.needle) {
			t.Errorf("展示页缺少 %s（%q）", want.desc, want.needle)
		}
	}

	// 确定性 + 落盘
	again, err := compile(t, p, builder.WithCollectionResolver(galleryCollection{}))
	if err != nil {
		t.Fatalf("二次编译失败: %v", err)
	}
	if again.HTML != html || again.CSS != c.CSS {
		t.Errorf("展示页产物不确定")
	}
	doc, err := builder.RenderDocument(c)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	out := filepath.Join(os.TempDir(), "cardstack-gallery.html")
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		t.Fatalf("写展示页失败: %v", err)
	}
	t.Logf("展示页已生成：%s（%.1f KB）", out, float64(len(doc))/1024)
}
