package unit

// cardstack_collection_test.go — 集合卡（core.cardstack）回归链路。
//
// 覆盖内容集合的完整路径：集合源 → 卡片数量 → 两种内容来源（内置字段映射 /
// 子节点模板 + item.* 绑定）→ 字段白名单校验 → 产物确定性。
// 这四条正是「卡片 = 容器 + 数据槽位」这个设计的契约面，改动它们等于改语义。

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

// shopCollection 假内容解析器：商品 + 文章两个集合源，并给出字段白名单
// （真实实现是 content 模块的 Service，这里只保留契约行为）。
type shopCollection struct{}

func (shopCollection) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	switch source {
	case "content:product":
		return []map[string]any{
			{"id": 1, "slug": "graphite", "name": "石墨黑", "price": "299", "images": []any{"https://x/1.jpg"}},
			{"id": 2, "slug": "emerald", "name": "松石绿", "price": "359", "images": []any{"https://x/2.jpg"}},
		}, nil
	case "content:article":
		return []map[string]any{
			{"id": 3, "slug": "first", "title": "第一篇", "excerpt": "第一篇摘要", "featuredImage": "https://x/a.jpg"},
			{"id": 4, "slug": "second", "title": "第二篇", "excerpt": "第二篇摘要", "featuredImage": "https://x/b.jpg"},
			{"id": 5, "slug": "third", "title": "第三篇", "excerpt": "第三篇摘要", "featuredImage": "https://x/c.jpg"},
		}, nil
	}
	return nil, fmt.Errorf("未知集合源 %q", source)
}

func (shopCollection) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{
		{Source: "content:product", Label: "商品列表", Fields: []string{"name", "description", "price", "images"}},
		{Source: "content:article", Label: "文章列表", Fields: []string{"title", "body", "excerpt", "featuredImage"}},
	}, nil
}

// pageWith 把单个 cardstack 节点包成最小页面：propsJSON 为 props 原文，
// childrenJSON 为子节点 JSON 数组（可空）。
func pageWith(propsJSON string, childrenJSON ...string) *builder.Page {
	kids := ""
	if len(childrenJSON) > 0 {
		kids = ",\"children\":[" + strings.Join(childrenJSON, ",") + "]"
	}
	// 页面设置给最小合法值（版心模式是必填项，空 settings 会被校验拒绝）。
	raw := `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px"}},"root":[{"id":"n1","type":"core.cardstack","props":` + propsJSON + kids + `}]}`
	p, err := builder.ParsePage([]byte(raw))
	if err != nil {
		panic(err)
	}
	return p
}

// TestCardstackCollectionFieldMapping 无子节点 → 组件自带字段映射出卡。
func TestCardstackCollectionFieldMapping(t *testing.T) {
	p := pageWith(`{"trigger":"hover","shape":"line","collectionSource":"content:article","collectionLimit":2,"cardTitleField":"title","cardTextField":"excerpt","cardImageField":"featuredImage","cardLinkField":"slug","cardLinkPrefix":"/article/"}`)
	c, err := compile(t, p, builder.WithCollectionResolver(shopCollection{}))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{"第一篇", "第二篇", "/article/first", "https://x/a.jpg"} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("字段映射模式产物缺少 %q", want)
		}
	}
	if strings.Contains(c.HTML, "第三篇") {
		t.Errorf("collectionLimit=2 应只取两条")
	}
}

// TestCardstackCollectionNodeTemplate 有子节点 → 子节点当模板，item.* 绑定取当前项。
func TestCardstackCollectionNodeTemplate(t *testing.T) {
	p := pageWith(
		`{"trigger":"hover","collectionSource":"content:article"}`,
		`{"id":"c1","type":"core.heading","props":{"text":"","tag":"h3","binding":{"field":"item.title","fallback":"（无标题）"}}}`,
		`{"id":"c2","type":"core.text","props":{"mode":"plaintext","plainTag":"p","text":"","binding":{"field":"item.excerpt"}}}`,
	)
	c, err := compile(t, p, builder.WithCollectionResolver(shopCollection{}))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 三篇文章 → 三张卡，每张卡的字段来自各自的集合项。
	for _, want := range []string{"第一篇", "第二篇", "第三篇", "第一篇摘要", "第三篇摘要"} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("子节点模板产物缺少 %q", want)
		}
	}
	if n := strings.Count(c.HTML, "sky-cardstack-card"); n < 3 {
		t.Errorf("应展开 3 张卡，实际出现 %d 处卡片标记", n)
	}
	// 子节点模板模式走的是子节点内容，不该混入字段映射模式的内置卡内元素。
	if strings.Contains(c.HTML, "sky-cardstack-link") {
		t.Errorf("子节点模板模式不应混入字段映射模式的卡内元素")
	}
}

// TestCardstackCollectionWhitelist 字段不在白名单 → 构建期报错并列出可用字段。
func TestCardstackCollectionWhitelist(t *testing.T) {
	p := pageWith(`{"trigger":"hover","collectionSource":"content:article","cardTitleField":"tittle"}`)
	_, err := compile(t, p, builder.WithCollectionResolver(shopCollection{}))
	if err == nil {
		t.Fatalf("字段名拼错应编译失败")
	}
	if !strings.Contains(err.Error(), "tittle") || !strings.Contains(err.Error(), "title") {
		t.Errorf("报错应指出错误字段并列出可用字段：%v", err)
	}
}

// TestCardstackCollectionDeterminism 集合卡同样满足确定性构建（同输入同字节）。
func TestCardstackCollectionDeterminism(t *testing.T) {
	p := pageWith(
		`{"trigger":"hover","shape":"line","collectionSource":"content:product","cardTitleField":"name","cardMetaField":"price"}`,
		`{"id":"c1","type":"core.text","props":{"mode":"plaintext","plainTag":"p","text":"","binding":{"field":"item.name"}}}`,
	)
	first, err := compile(t, p, builder.WithCollectionResolver(shopCollection{}))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	second, err := compile(t, p, builder.WithCollectionResolver(shopCollection{}))
	if err != nil {
		t.Fatalf("二次编译失败: %v", err)
	}
	if first.HTML != second.HTML || first.CSS != second.CSS {
		t.Errorf("集合卡产物不确定：同输入两次编译字节不一致")
	}
}
