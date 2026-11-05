package core

import (
	"context"
	"strings"
	"testing"
)

type stubCollection struct {
	sources []CollectionSchema
	items   map[string][]map[string]any
	schema  error
}

func (s stubCollection) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	if items, ok := s.items[source]; ok {
		return items, nil
	}
	return nil, nil
}

func (s stubCollection) CollectionSchemas(context.Context) ([]CollectionSchema, error) {
	if s.schema != nil {
		return nil, s.schema
	}
	return s.sources, nil
}

func TestCollectionRegistryResolveBySource(t *testing.T) {
	reg := NewCollectionRegistry()
	content := stubCollection{
		sources: []CollectionSchema{{Source: "content:article", Fields: []string{"title"}}},
		items:   map[string][]map[string]any{"content:article": {{"id": "a1", "title": "第一篇"}}},
	}
	product := stubCollection{
		sources: []CollectionSchema{{Source: "content:product", Fields: []string{"name"}}},
		items:   map[string][]map[string]any{"content:product": {{"id": "p1", "name": "衬衫"}}},
	}
	if err := reg.Register(content); err != nil {
		t.Fatalf("注册内容集合源失败: %v", err)
	}
	if err := reg.Register(product); err != nil {
		t.Fatalf("注册商品集合源失败: %v", err)
	}
	items, err := reg.ResolveCollection(context.Background(), "content:product", nil)
	if err != nil {
		t.Fatalf("解析商品集合失败: %v", err)
	}
	if len(items) != 1 || items[0]["name"] != "衬衫" {
		t.Fatalf("解析结果应来自商品提供方: %+v", items)
	}
	items, err = reg.ResolveCollection(context.Background(), "content:article", nil)
	if err != nil {
		t.Fatalf("解析内容集合失败: %v", err)
	}
	if len(items) != 1 || items[0]["title"] != "第一篇" {
		t.Fatalf("解析结果应来自内容提供方: %+v", items)
	}
}

func TestCollectionRegistryUnknownSource(t *testing.T) {
	reg := NewCollectionRegistry()
	if err := reg.Register(stubCollection{sources: []CollectionSchema{
		{Source: "content:article"}, {Source: "content:product"},
	}}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	_, err := reg.ResolveCollection(context.Background(), "content:order", nil)
	if err == nil {
		t.Fatalf("未知集合源应报错")
	}
	if !strings.Contains(err.Error(), "content:order") ||
		!strings.Contains(err.Error(), "content:article") ||
		!strings.Contains(err.Error(), "content:product") {
		t.Fatalf("报错应指出未知源并列出可用源: %v", err)
	}
}

type stubOptionsCollection struct {
	stubCollection
	options CollectionFilterOptions
	seen    []string
}

func (s *stubOptionsCollection) CollectionFilterOptions(_ context.Context, source, projectID string) (CollectionFilterOptions, error) {
	s.seen = append(s.seen, source+"|"+projectID)
	return s.options, nil
}

func TestCollectionRegistryFilterOptionsBySource(t *testing.T) {
	reg := NewCollectionRegistry()
	plain := stubCollection{sources: []CollectionSchema{{Source: "content:article"}}}
	withOptions := &stubOptionsCollection{
		stubCollection: stubCollection{sources: []CollectionSchema{{Source: "content:product"}}},
		options:        CollectionFilterOptions{Brands: []CollectionFilterChoice{{ID: "b1", Name: "Alibarbar"}}},
	}
	if err := reg.Register(plain); err != nil {
		t.Fatalf("注册内容集合源失败: %v", err)
	}
	if err := reg.Register(withOptions); err != nil {
		t.Fatalf("注册商品集合源失败: %v", err)
	}

	ctx := context.Background()
	opts, err := reg.CollectionFilterOptions(ctx, "content:product", "proj-1")
	if err != nil {
		t.Fatalf("取商品筛选选项失败: %v", err)
	}
	if len(opts.Brands) != 1 || opts.Brands[0].ID != "b1" {
		t.Fatalf("筛选选项应来自为该源注册的提供方: %+v", opts)
	}
	if len(withOptions.seen) != 1 || withOptions.seen[0] != "content:product|proj-1" {
		t.Fatalf("转发应带上查询的源与工程 ID: %v", withOptions.seen)
	}

	if opts, err := reg.CollectionFilterOptions(ctx, "content:article", "proj-1"); err != nil || len(opts.Brands)+len(opts.Tags)+len(opts.Categories)+len(opts.Attributes) != 0 {
		t.Fatalf("无能力的源应返回空选项且不报错: %+v / %v", opts, err)
	}

	if opts, err := reg.CollectionFilterOptions(ctx, "content:order", "proj-1"); err != nil || len(opts.Brands) != 0 {
		t.Fatalf("未注册的源应返回空选项且不报错: %+v / %v", opts, err)
	}

	if _, err := reg.CollectionFilterOptions(ctx, "   ", "proj-1"); err != nil {
		t.Fatalf("空源应返回空选项且不报错: %v", err)
	}
}

func TestCollectionRegistrySchemasDeterministic(t *testing.T) {
	first := NewCollectionRegistry()
	if err := first.Register(stubCollection{sources: []CollectionSchema{{Source: "content:product"}}}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if err := first.Register(stubCollection{sources: []CollectionSchema{{Source: "content:article"}}}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	second := NewCollectionRegistry()
	if err := second.Register(stubCollection{sources: []CollectionSchema{{Source: "content:article"}}}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if err := second.Register(stubCollection{sources: []CollectionSchema{{Source: "content:product"}}}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	a, err := first.CollectionSchemas(context.Background())
	if err != nil {
		t.Fatalf("取元数据失败: %v", err)
	}
	b, err := second.CollectionSchemas(context.Background())
	if err != nil {
		t.Fatalf("取元数据失败: %v", err)
	}
	if len(a) != 2 || a[0].Source != "content:article" || a[1].Source != "content:product" {
		t.Fatalf("元数据应按源标识字典序: %+v", a)
	}
	if len(b) != 2 || b[0].Source != "content:article" || b[1].Source != "content:product" {
		t.Fatalf("元数据顺序不应随注册顺序变化: %+v", b)
	}
	sources, err := first.Sources(context.Background())
	if err != nil {
		t.Fatalf("取源清单失败: %v", err)
	}
	if strings.Join(sources, ",") != "content:article,content:product" {
		t.Fatalf("源清单顺序应确定: %v", sources)
	}
}

func TestCollectionRegistryRejectsBadRegistration(t *testing.T) {
	t.Run("nil 提供方", func(t *testing.T) {
		if err := NewCollectionRegistry().Register(nil); err == nil {
			t.Fatalf("nil 提供方应被拒绝")
		}
	})
	t.Run("未声明集合源", func(t *testing.T) {
		if err := NewCollectionRegistry().Register(stubCollection{}); err == nil {
			t.Fatalf("未声明集合源应被拒绝")
		}
	})
	t.Run("源标识含首尾空白", func(t *testing.T) {
		reg := NewCollectionRegistry()
		if err := reg.Register(stubCollection{sources: []CollectionSchema{{Source: " content:product "}}}); err == nil {
			t.Fatalf("非法源标识应被拒绝")
		}
	})
	t.Run("元数据不可用", func(t *testing.T) {
		reg := NewCollectionRegistry()
		if err := reg.Register(stubCollection{schema: errStubResolver}); err == nil {
			t.Fatalf("元数据取不到应被拒绝")
		}
	})
	t.Run("跨提供方重复源", func(t *testing.T) {
		reg := NewCollectionRegistry()
		if err := reg.Register(stubCollection{sources: []CollectionSchema{{Source: "content:product"}}}); err != nil {
			t.Fatalf("首次注册失败: %v", err)
		}
		err := reg.Register(stubCollection{sources: []CollectionSchema{{Source: "content:product"}}})
		if err == nil || !strings.Contains(err.Error(), "重复") {
			t.Fatalf("重复源应被拒绝: %v", err)
		}
	})
}

func TestCollectionRegistryProviderSources(t *testing.T) {
	reg := NewCollectionRegistry()
	multi := stubCollection{
		sources: []CollectionSchema{{Source: "content:article"}, {Source: "content:category"}},
		items: map[string][]map[string]any{
			"content:article":  {{"id": "a1"}},
			"content:category": {{"id": "c1"}},
		},
	}
	if err := reg.Register(multi); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	for _, source := range []string{"content:article", "content:category"} {
		if _, err := reg.ResolveCollection(context.Background(), source, nil); err != nil {
			t.Fatalf("源 %s 应可解析: %v", source, err)
		}
	}
}

func TestBuildContextProjectAndLang(t *testing.T) {
	ctx := WithBuildProjectID(WithBuildLang(context.Background(), "en-US"), " proj-1 ")
	if got := BuildProjectID(ctx); got != "proj-1" {
		t.Fatalf("工程 ID 应去首尾空白: %q", got)
	}
	if got := BuildLang(ctx); got != "en-US" {
		t.Fatalf("语言应可读回: %q", got)
	}

	if got := BuildProjectID(nil); got != "" {
		t.Fatalf("nil ctx 应返回空工程 ID: %q", got)
	}
	if got := BuildProjectID(WithBuildProjectID(nil, "p")); got != "p" {
		t.Fatalf("nil ctx 注入后应可读回: %q", got)
	}
}
