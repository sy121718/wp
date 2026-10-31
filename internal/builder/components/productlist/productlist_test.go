package productlist

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

type fakeCollection struct {
	items  []map[string]any
	Source string
	Filter map[string]string
}

func (f *fakeCollection) ResolveCollection(_ context.Context, source string, filter map[string]string) ([]map[string]any, error) {
	f.Source, f.Filter = source, filter
	return f.items, nil
}

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

func nodeOf(t *testing.T, kv map[string]any) *core.Node {
	t.Helper()
	return &core.Node{ID: "list1", Type: Type, Props: propsOf(t, kv)}
}

func itemOf(slug, name, createdAt string) map[string]any {
	return map[string]any{
		"name":         name,
		"images":       []any{"/storage/" + slug + ".jpg"},
		"imageAlt":     name + "主图",
		"priceRange":   "99 ~ 199",
		"comparePrice": "259",
		"tags":         `["新品"]`,
		"slug":         slug,
		"createdAt":    createdAt,
	}
}

func cardFields() map[string]any {
	return map[string]any{
		"imageField":        "item.images",
		"imageAltField":     "item.imageAlt",
		"titleField":        "item.name",
		"priceField":        "item.priceRange",
		"comparePriceField": "item.comparePrice",
		"tagsField":         "item.tags",
		"linkField":         "item.slug",
		"linkPrefix":        "/products/",
	}
}

func withFields(extra map[string]any) map[string]any {
	out := cardFields()
	out["collectionSource"] = collectionSourceProduct
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestValidateRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		kv   map[string]any
		want string
	}{
		{"一个字段都没声明", map[string]any{"collectionLimit": 4}, "至少需要声明一个商品字段"},
		{"字段前缀不合法", map[string]any{"titleField": "category.name"}, "无效的字段路径"},
		{"驼峰字段名合法但前缀错", map[string]any{"titleField": "item."}, "无效的字段路径"},
		{"非商品集合源", map[string]any{"titleField": "item.name", "collectionSource": "content:article"}, "无效的集合源"},
		{"非法布局", map[string]any{"titleField": "item.name", "layout": "masonry"}, "无效的布局"},
		{"非法排序", map[string]any{"titleField": "item.name", "orderBy": "price"}, "无效的排序"},
		{"取几条越界", map[string]any{"titleField": "item.name", "collectionLimit": maxLimit + 1}, "取几条必须在"},
		{"标题层级越界", map[string]any{"titleField": "item.name", "titleTag": "h1"}, "标题层级"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := decodePropsOf(t, c.kv)
			err := validateExtra(&p, "n1")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("应报 %q，实际 %v", c.want, err)
			}
		})
	}

	good := decodePropsOf(t, withFields(map[string]any{"priceField": "item.priceRange"}))
	if err := validateExtra(&good, "n1"); err != nil {
		t.Fatalf("合法配置不该被拒: %v", err)
	}
}

func TestFieldBindingsTranslatePrefix(t *testing.T) {
	comp := &Component{}
	node := nodeOf(t, map[string]any{
		"imageField": "item.images",
		"titleField": "product.name",
		"priceField": "item.priceRange",
	})
	refs, err := comp.FieldBindings(node)
	if err != nil {
		t.Fatalf("收集字段绑定失败: %v", err)
	}
	want := []string{"images", "name", "priceRange"}
	if len(refs) != len(want) {
		t.Fatalf("应自报 %d 个绑定，实际 %d（%+v）", len(want), len(refs), refs)
	}
	for i, w := range want {
		if refs[i].EntityType != "product" || refs[i].Field != w {
			t.Fatalf("第 %d 个绑定应为 product.%s，实际 %+v", i, w, refs[i])
		}
	}
}

func TestBuildViewPushesFilterAndMapsCards(t *testing.T) {
	coll := &fakeCollection{items: []map[string]any{
		itemOf("shirt", "夏季衬衫", "2026-01-02T03:04:05Z"),
		itemOf("coat", "冬季外套", "2026-02-03T04:05:06Z"),
	}}
	p := decodePropsOf(t, withFields(map[string]any{
		"collectionLimit":  1,
		"filterStatus":     "published",
		"filterCategoryID": "11111111-1111-1111-1111-111111111111",
	}))
	view, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: coll})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}

	if coll.Source != collectionSourceProduct {
		t.Fatalf("集合源应下推 %s，实际 %q", collectionSourceProduct, coll.Source)
	}
	if coll.Filter[filterKeyStatus] != "published" || coll.Filter[filterKeyCategoryID] == "" {
		t.Fatalf("筛选维度未完整下推: %+v", coll.Filter)
	}
	if _, ok := coll.Filter[filterKeyTagID]; ok {
		t.Fatalf("未声明的维度不该下推: %+v", coll.Filter)
	}

	if len(view.Cards) != 1 {
		t.Fatalf("取几条=1 应只留一张卡，实际 %d", len(view.Cards))
	}
	card := view.Cards[0]
	if !card.HasImage || card.ImageURL != "/storage/shirt.jpg" || card.ImageAlt != "夏季衬衫主图" {
		t.Fatalf("主图映射不符: %+v", card)
	}
	if !card.HasTitle || card.Title != "夏季衬衫" || card.TitleTag != defaultTitleTag {
		t.Fatalf("标题映射不符: %+v", card)
	}

	if card.Price != "¥99 ~ 199" || card.ComparePrice != "¥ 259.00" || !card.HasComparePrice {
		t.Fatalf("价格映射不符: %+v", card)
	}
	if len(card.Tags) != 1 || card.Tags[0] != "新品" {
		t.Fatalf("标签映射不符: %+v", card.Tags)
	}
	if card.Href != "/products/shirt" {
		t.Fatalf("链接应为前缀 + slug，实际 %q", card.Href)
	}
}

func TestBuildViewSortsBeforeLimit(t *testing.T) {
	items := []map[string]any{
		itemOf("old", "旧货", "2026-01-01T00:00:00Z"),
		itemOf("new", "新货", "2026-03-01T00:00:00Z"),
		itemOf("mid", "中货", "2026-02-01T00:00:00Z"),
	}
	for _, c := range []struct{ order, want string }{{"newest", "新货"}, {"oldest", "旧货"}} {
		coll := &fakeCollection{items: append([]map[string]any(nil), items...)}
		p := decodePropsOf(t, withFields(map[string]any{"orderBy": c.order, "collectionLimit": 1}))
		view, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: coll})
		if err != nil {
			t.Fatalf("BuildView(%s): %v", c.order, err)
		}
		if len(view.Cards) != 1 || view.Cards[0].Title != c.want {
			t.Fatalf("orderBy=%s 应取到 %q，实际 %+v", c.order, c.want, view.Cards)
		}
	}

	coll := &fakeCollection{items: append([]map[string]any(nil), items...)}
	p := decodePropsOf(t, withFields(nil))
	view, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: coll})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Cards[0].Title != "旧货" {
		t.Fatalf("默认序不该重排，实际首项 %q", view.Cards[0].Title)
	}
}

func TestBuildViewEmpty(t *testing.T) {
	coll := &fakeCollection{}
	p := decodePropsOf(t, withFields(map[string]any{"emptyText": "该分类下还没有商品"}))
	view, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: coll})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !view.Empty || view.EmptyText != "该分类下还没有商品" || len(view.Cards) != 0 {
		t.Fatalf("空集合应渲染自定义空态: %+v", view)
	}

	p2 := decodePropsOf(t, withFields(nil))
	view2, _ := BuildView(nodeOf(t, nil), &p2, &core.RenderContext{Collection: coll})
	if view2.EmptyText != defaultEmptyText {
		t.Fatalf("空态缺省文案应生效，实际 %q", view2.EmptyText)
	}
}

func TestBuildViewUnselectedSource(t *testing.T) {
	p := decodePropsOf(t, withFields(map[string]any{"collectionSource": ""}))
	view, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{})
	if err != nil {
		t.Fatalf("未选数据源不该报错: %v", err)
	}
	if !view.Empty || view.EmptyText != defaultEmptyText {
		t.Fatalf("未选数据源应渲染空态: %+v", view)
	}
}

func TestBuildViewRequiresCollection(t *testing.T) {
	p := decodePropsOf(t, withFields(nil))
	if _, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{}); err == nil {
		t.Fatalf("缺集合解析器应报错")
	}
}

func TestColumnsDecl(t *testing.T) {
	if got := columnsDecl(LayoutList, ColumnsAuto); got != "1fr" {
		t.Fatalf("列表布局应单列，实际 %q", got)
	}
	if got := columnsDecl(LayoutGrid, "3"); got != "repeat(3, minmax(0, 1fr))" {
		t.Fatalf("三列声明不符: %q", got)
	}
	auto := columnsDecl(LayoutGrid, ColumnsAuto)
	if !strings.Contains(auto, "min(100%") || !strings.Contains(auto, "auto-fill") {
		t.Fatalf("自适应列应折行且宽度不写死: %q", auto)
	}
}

func TestOptionFilterProps(t *testing.T) {

	p := decodePropsOf(t, withFields(map[string]any{"filterOptionKey": "color", "filterOptionValue": "red"}))
	if err := validateExtra(&p, "n1"); err != nil {
		t.Fatalf("成对的属性筛选应通过校验: %v", err)
	}
	coll := &fakeCollection{}
	if _, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: coll}); err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if coll.Filter[optionFilterPrefix+"color"] != "red" {
		t.Fatalf("属性维度应下推为 option.color，实际 %+v", coll.Filter)
	}

	half := decodePropsOf(t, withFields(map[string]any{"filterOptionKey": "color"}))
	if err := validateExtra(&half, "n1"); err == nil {
		t.Fatalf("只填属性 key 应被拒绝")
	}

	bad := decodePropsOf(t, withFields(map[string]any{"filterOptionKey": "color", "filterOptionValue": "红 色"}))
	if err := validateExtra(&bad, "n1"); err == nil {
		t.Fatalf("非法形状的属性值应被拒绝")
	}
}
