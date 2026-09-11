package productcard

// productcard_test.go — 商品卡组件的单元测试（issue #22）。
//
// 覆盖四类断言：
//
//	1. 配置期校验（至少一个字段 / 字段路径形状 / 标题层级）；
//	2. 字段绑定自报（item.* 与 product.* 都翻译成 product.*，交注册表按同一份白名单判定）；
//	3. 视图组装（有值才输出节点、JSON 数组取首元素、标签数组解析、链接前缀拼接）；
//	4. 越界 / 解析失败必须上抛，不静默出空卡。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// propsOf 把 props 键值编码为节点 props JSON。
func propsOf(t *testing.T, kv map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("props 编码失败: %v", err)
	}
	return b
}

// decodePropsOf 解码 props 为组件 Props。
func decodePropsOf(t *testing.T, kv map[string]any) Props {
	t.Helper()
	var p Props
	if err := json.Unmarshal(propsOf(t, kv), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	return p
}

// stubResolver 固定字段值映射的解析器（替代构建期解析器）。
type stubResolver struct {
	values map[string]string
	errOn  string
}

// ResolveString 实现 core.ContentResolver。
func (s stubResolver) ResolveString(field string) (string, error) {
	if s.errOn != "" && field == s.errOn {
		return "", errors.New("字段越界")
	}
	return s.values[field], nil
}

// TestValidateRequiresDeclaredField 一个字段都没声明时拒绝：空卡片发不出去。
func TestValidateRequiresDeclaredField(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleTag": "h3"})
	if err := validateExtra(&p, "n1"); err == nil || !strings.Contains(err.Error(), "至少需要声明一个商品字段") {
		t.Fatalf("空槽位应被拒，实际 %v", err)
	}
}

// TestValidateRejectsBadFieldPath 字段路径：前缀只认 item / product，字段名形状受限。
func TestValidateRejectsBadFieldPath(t *testing.T) {
	bad := []string{
		"name",          // 缺前缀
		"category.name", // 不支持的实体前缀（分类该由自己的组件承担）
		"item.Name",     // 字段名大写
		"item.",         // 空字段名
		"item.1st",      // 数字开头
	}
	for _, field := range bad {
		p := decodePropsOf(t, map[string]any{"titleField": field})
		if err := validateExtra(&p, "n1"); err == nil {
			t.Fatalf("字段路径 %q 应被拒", field)
		}
	}
	// 标题层级白名单。
	p := decodePropsOf(t, map[string]any{"titleField": "item.name", "titleTag": "h1"})
	if err := validateExtra(&p, "n1"); err == nil || !strings.Contains(err.Error(), "标题层级") {
		t.Fatalf("h1 超出卡片层级白名单，应被拒，实际 %v", err)
	}
}

// TestFieldBindingsTranslatePrefix 字段绑定自报：两种前缀都翻译成 product.<字段>。
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

// TestBuildViewRendersDeclaredFields 视图组装：声明什么字段就有什么，货币前缀与标题层级生效。
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
	view, err := BuildView(&p, resolver)
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

// TestBuildViewSkipsEmptyFields 空值不输出空壳节点（模板据此不渲染）。
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
	}})
	if err != nil {
		t.Fatalf("组装视图失败: %v", err)
	}
	if view.HasImage || view.HasPrice || view.HasComparePrice || len(view.Tags) != 0 {
		t.Fatalf("未填字段不该产生节点: %+v", view)
	}
	if !view.HasTitle || view.Title != "只有标题" {
		t.Fatalf("已填字段应保留: %+v", view)
	}
	// 无图 → 无 alt（alt 只对图有意义）。
	if view.ImageAlt != "" {
		t.Fatalf("无图时不该有 alt，实际 %q", view.ImageAlt)
	}
}

// TestBuildViewImageFromJSONArray 实体绑定的 images 是 JSON 文本：取首元素。
func TestBuildViewImageFromJSONArray(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"imageField": "product.images", "titleField": "product.name"})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.images": `["/storage/a.jpg","/storage/b.jpg"]`,
		"product.name":   "外套",
	}})
	if err != nil {
		t.Fatalf("组装视图失败: %v", err)
	}
	if !view.HasImage || view.ImageURL != "/storage/a.jpg" {
		t.Fatalf("JSON 数组应取首元素，实际 %+v", view)
	}
	// 没有 alt 字段时用标题兜底（图必须有可读的替代文本）。
	if view.ImageAlt != "外套" {
		t.Fatalf("alt 缺失应回退标题，实际 %q", view.ImageAlt)
	}
}

// TestBuildViewHrefVariants 链接三种写法都不该拼出坏链接。
func TestBuildViewHrefVariants(t *testing.T) {
	cases := []struct{ prefix, value, want string }{
		{"/products/", "shirt", "/products/shirt"},
		{"/products", "shirt", "/products/shirt"},
		{"/products/", "/products/shirt", "/products/shirt"}, // 已是站内绝对路径：不再拼前缀
		{"/products/", "https://cdn.example.com/p/shirt", "https://cdn.example.com/p/shirt"},
		{"", "shirt", "shirt"},
		{"/products/", "javascript:alert(1)", ""}, // 协议不在白名单：不输出链接
	}
	for _, c := range cases {
		p := decodePropsOf(t, map[string]any{
			"titleField": "item.name", "linkField": "item.slug", "linkPrefix": c.prefix,
		})
		view, err := BuildView(&p, stubResolver{values: map[string]string{
			"item.name": "衬衫", "item.slug": c.value,
		}})
		if err != nil {
			t.Fatalf("组装视图失败: %v", err)
		}
		if view.Href != c.want {
			t.Fatalf("prefix=%q value=%q 应得到 %q，实际 %q", c.prefix, c.value, c.want, view.Href)
		}
	}
}

// TestBuildViewPropagatesResolveError 解析失败（越界字段）必须上抛：不静默出空卡。
func TestBuildViewPropagatesResolveError(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "item.bogus"})
	if _, err := BuildView(&p, stubResolver{errOn: "item.bogus"}); err == nil {
		t.Fatalf("越界字段应上抛解析错误")
	}
}

// TestBuildViewRequiresResolver 缺解析器（装配缺陷）时报错而不是渲染空卡。
func TestBuildViewRequiresResolver(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "item.name"})
	if _, err := BuildView(&p, nil); err == nil {
		t.Fatalf("缺解析器应报错")
	}
}
