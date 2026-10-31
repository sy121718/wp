package productcard

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func propsOf(t *testing.T, kv map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("props 编码失败: %v", err)
	}
	return b
}

func decodePropsOf(t *testing.T, kv map[string]any) Props {
	t.Helper()
	var p Props
	if err := json.Unmarshal(propsOf(t, kv), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	return p
}

type stubResolver struct {
	values map[string]string
	errOn  string
}

func (s stubResolver) ResolveString(field string) (string, error) {
	if s.errOn != "" && field == s.errOn {
		return "", errors.New("字段越界")
	}
	return s.values[field], nil
}

func TestValidateRequiresDeclaredField(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleTag": "h3"})
	if err := validateExtra(&p, "n1"); err == nil || !strings.Contains(err.Error(), "至少需要声明一个商品字段") {
		t.Fatalf("空槽位应被拒，实际 %v", err)
	}
}

func TestValidateRejectsBadFieldPath(t *testing.T) {
	bad := []string{
		"name",
		"category.name",
		"item.Name",
		"item.",
		"item.1st",
	}
	for _, field := range bad {
		p := decodePropsOf(t, map[string]any{"titleField": field})
		if err := validateExtra(&p, "n1"); err == nil {
			t.Fatalf("字段路径 %q 应被拒", field)
		}
	}

	p := decodePropsOf(t, map[string]any{"titleField": "item.name", "titleTag": "h1"})
	if err := validateExtra(&p, "n1"); err == nil || !strings.Contains(err.Error(), "标题层级") {
		t.Fatalf("h1 超出卡片层级白名单，应被拒，实际 %v", err)
	}
}

func TestFieldBindingsTranslatePrefix(t *testing.T) {
	comp := &Component{}
	node := &core.Node{ID: "n1", Type: Type, Props: propsOf(t, map[string]any{
		"imageField":        "item.images",
		"titleField":        "item.name",
		"priceField":        "product.priceRange",
		"comparePriceField": "item.comparePrice",
		"tagsField":         "item.tags",
		"linkField":         "item.slug",
	})}
	refs, err := comp.FieldBindings(node)
	if err != nil {
		t.Fatalf("收集字段绑定失败: %v", err)
	}
	want := []string{"images", "name", "priceRange", "comparePrice", "tags", "slug"}
	if len(refs) != len(want) {
		t.Fatalf("应自报 %d 个字段绑定，实际 %d（%+v）", len(want), len(refs), refs)
	}
	for i, w := range want {
		if refs[i].EntityType != "product" || refs[i].Field != w {
			t.Fatalf("第 %d 个绑定应为 product.%s，实际 %s.%s", i, w, refs[i].EntityType, refs[i].Field)
		}
	}
}

func TestBuildViewRendersDeclaredFields(t *testing.T) {
	p := decodePropsOf(t, map[string]any{
		"imageField":        "item.images",
		"imageAltField":     "item.imageAlt",
		"titleField":        "item.name",
		"priceField":        "item.priceRange",
		"comparePriceField": "item.comparePrice",
		"tagsField":         "item.tags",
		"linkField":         "item.slug",
		"linkPrefix":        "/products/",
		"currency":          "¥",
		"titleTag":          "h4",
	})
	resolver := stubResolver{values: map[string]string{
		"item.images":       "/storage/shirt.jpg",
		"item.imageAlt":     "蓝色衬衫",
		"item.name":         "夏季衬衫",
		"item.priceRange":   "99 ~ 199",
		"item.comparePrice": "259",
		"item.tags":         `["新品","纯棉"]`,
		"item.slug":         "summer-shirt",
	}}
	view, err := BuildView(&p, resolver, nil)
	if err != nil {
		t.Fatalf("组装视图失败: %v", err)
	}
	if !view.HasImage || view.ImageURL != "/storage/shirt.jpg" || view.ImageAlt != "蓝色衬衫" {
		t.Fatalf("主图与 alt 不符: %+v", view)
	}
	if !view.HasTitle || view.Title != "夏季衬衫" || view.TitleTag != "h4" {
		t.Fatalf("标题不符: %+v", view)
	}
	if view.Price != "¥99 ~ 199" || view.ComparePrice != "¥259" {
		t.Fatalf("价格口径不符: %+v", view)
	}
	if len(view.Tags) != 2 || view.Tags[0] != "新品" {
		t.Fatalf("标签解析不符: %+v", view.Tags)
	}
	if view.Href != "/products/summer-shirt" {
		t.Fatalf("链接应拼上前缀，实际 %q", view.Href)
	}
}

func TestBuildViewSkipsEmptyFields(t *testing.T) {
	p := decodePropsOf(t, map[string]any{
		"imageField":        "item.images",
		"titleField":        "item.name",
		"priceField":        "item.priceRange",
		"comparePriceField": "item.comparePrice",
		"tagsField":         "item.tags",
	})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"item.name": "只有标题",
	}}, nil)
	if err != nil {
		t.Fatalf("组装视图失败: %v", err)
	}
	if view.HasImage || view.HasPrice || view.HasComparePrice || len(view.Tags) != 0 {
		t.Fatalf("未填字段不该产生节点: %+v", view)
	}
	if !view.HasTitle || view.Title != "只有标题" {
		t.Fatalf("已填字段应保留: %+v", view)
	}

	if view.ImageAlt != "" {
		t.Fatalf("无图时不该有 alt，实际 %q", view.ImageAlt)
	}
}

func TestBuildViewImageFromJSONArray(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"imageField": "product.images", "titleField": "product.name"})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.images": `["/storage/a.jpg","/storage/b.jpg"]`,
		"product.name":   "外套",
	}}, nil)
	if err != nil {
		t.Fatalf("组装视图失败: %v", err)
	}
	if !view.HasImage || view.ImageURL != "/storage/a.jpg" {
		t.Fatalf("JSON 数组应取首元素，实际 %+v", view)
	}

	if view.ImageAlt != "外套" {
		t.Fatalf("alt 缺失应回退标题，实际 %q", view.ImageAlt)
	}
}

func TestBuildViewHrefVariants(t *testing.T) {
	cases := []struct{ prefix, value, want string }{
		{"/products/", "shirt", "/products/shirt"},
		{"/products", "shirt", "/products/shirt"},
		{"/products/", "/products/shirt", "/products/shirt"},
		{"/products/", "https://cdn.example.com/p/shirt", "https://cdn.example.com/p/shirt"},
		{"", "shirt", "shirt"},
		{"/products/", "javascript:alert(1)", ""},
	}
	for _, c := range cases {
		p := decodePropsOf(t, map[string]any{
			"titleField": "item.name", "linkField": "item.slug", "linkPrefix": c.prefix,
		})
		view, err := BuildView(&p, stubResolver{values: map[string]string{
			"item.name": "衬衫", "item.slug": c.value,
		}}, nil)
		if err != nil {
			t.Fatalf("组装视图失败: %v", err)
		}
		if view.Href != c.want {
			t.Fatalf("prefix=%q value=%q 应得到 %q，实际 %q", c.prefix, c.value, c.want, view.Href)
		}
	}
}

func TestBuildViewPropagatesResolveError(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "item.bogus"})
	if _, err := BuildView(&p, stubResolver{errOn: "item.bogus"}, nil); err == nil {
		t.Fatalf("越界字段应上抛解析错误")
	}
}

func TestBuildViewRequiresResolver(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "item.name"})
	if _, err := BuildView(&p, nil, nil); err == nil {
		t.Fatalf("缺解析器应报错")
	}
}
