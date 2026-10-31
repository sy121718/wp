package unit

import (
	"go_wp/pkg/upload"

	"context"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
)

func TestCollectionFieldWhitelistExcludesBody(t *testing.T) {
	full := contentcontract.FieldWhitelist("article")
	coll := contentcontract.CollectionFieldWhitelist("article")

	if !contains(full, "body") {
		t.Fatalf("单实体白名单必须保留 body（详情页要渲染正文）: %v", full)
	}
	for _, excluded := range []string{"body", "focusKeyword"} {
		if contains(coll, excluded) {
			t.Fatalf("集合白名单不应包含 %s: %v", excluded, coll)
		}
		if contentcontract.IsCollectionField("article", excluded) {
			t.Fatalf("IsCollectionField(%s) 应为 false", excluded)
		}
	}
	for _, kept := range []string{"title", "excerpt", "featuredImage", "seoTitle", "seoDescription"} {
		if !contains(coll, kept) {
			t.Fatalf("集合白名单应保留 %s: %v", kept, coll)
		}
	}
	if len(coll) != len(full)-2 {
		t.Fatalf("集合白名单应恰为数据源白名单减去两个字段: full=%d coll=%d", len(full), len(coll))
	}

	coll[0] = "__mutated__"
	if contentcontract.CollectionFieldWhitelist("article")[0] == "__mutated__" {
		t.Fatal("集合白名单返回了内部切片，调用方可篡改定义")
	}
}

func TestResolveCollectionOmitsBody(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	created, err := svc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "collection-projection",
		Data: map[string]any{
			"title":         "投影测试",
			"excerpt":       "摘要",
			"body":          "<p>这段正文有几千字，集合卡片一个字节都用不到</p>",
			"featuredImage": "/img/cover.jpg",
			"seoTitle":      "SEO 标题",
			"focusKeyword":  "投影",
		},
	})
	if err != nil {
		t.Fatalf("创建内容失败: %v", err)
	}

	items, err := svc.ResolveCollection(ctx, "content:article", nil)
	if err != nil {
		t.Fatalf("解析集合失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("应解析出 1 个集合项: %d", len(items))
	}
	item := items[0]
	for _, banned := range []string{"body", "focusKeyword"} {
		if _, ok := item[banned]; ok {
			t.Fatalf("集合项不应包含 %s: %+v", banned, item)
		}
	}

	for k, want := range map[string]any{
		"id": created.ID, "slug": "collection-projection",
		"title": "投影测试", "excerpt": "摘要",
		"featuredImage": upload.StorageURL("/img/cover.jpg"),
	} {
		if got := item[k]; got != want {
			t.Fatalf("集合项字段 %s 不一致: %v（期望 %v）", k, got, want)
		}
	}
}

func TestResolveCollectionFilterPushdownMatchesMemorySemantics(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	seed := func(slug, title, excerpt string) {
		t.Helper()
		if _, err := svc.Create(ctx, &contentdto.CreateReq{
			EntityType: "article", Slug: slug,
			Data: map[string]any{"title": title, "excerpt": excerpt, "body": "正文"},
		}); err != nil {
			t.Fatalf("创建内容失败: %v", err)
		}
	}
	seed("pushdown-a", "同题", "甲")
	seed("pushdown-b", "同题", "乙")
	seed("pushdown-c", "异题", "甲")

	one, err := svc.ResolveCollection(ctx, "content:article", map[string]string{"title": "同题"})
	if err != nil {
		t.Fatalf("解析集合失败: %v", err)
	}
	if len(one) != 2 {
		t.Fatalf("按 title 过滤应命中 2 条: %d", len(one))
	}

	both, err := svc.ResolveCollection(ctx, "content:article", map[string]string{"title": "同题", "excerpt": "乙"})
	if err != nil {
		t.Fatalf("解析集合失败: %v", err)
	}
	if len(both) != 1 || both[0]["slug"] != "pushdown-b" {
		t.Fatalf("多条件应 AND 收敛到 pushdown-b: %+v", both)
	}

	none, err := svc.ResolveCollection(ctx, "content:article", map[string]string{"title": "不存在"})
	if err != nil {
		t.Fatalf("空结果不应报错: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("不应命中任何条目: %+v", none)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
