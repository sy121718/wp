package unit

import (
	"context"
	"strings"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"

	"go_wp/public/test/support"
)

func newService(t *testing.T) *contentservice.Service {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := db.AutoMigrate(&contentmodel.Entity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	return contentservice.NewService(contentmodel.NewModel(db))
}

func TestContentCRUD(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "summer-story",
		Data: map[string]any{"title": "夏季故事", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if res.Revision != 1 || res.Slug != "summer-story" {
		t.Fatalf("创建响应错误: %+v", res)
	}
	id := res.ID

	res2, err := svc.Update(ctx, &contentdto.UpdateReq{
		ID: id, Data: map[string]any{"title": "夏季故事改", "excerpt": "摘要 v2"},
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if res2.Revision != 2 {
		t.Fatalf("revision 应递增为 2: %d", res2.Revision)
	}

	if err := svc.Delete(ctx, &contentdto.DeleteReq{ID: id}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := svc.Get(ctx, &contentdto.GetReq{ID: id}); err == nil {
		t.Fatalf("删除后应查不到")
	}
}

func TestContentRejectsForeignField(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	_, err := svc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "x",
		Data: map[string]any{"title": "ok", "evil": "injected"},
	})
	if err == nil || !strings.Contains(err.Error(), contentenums.ErrInvalidField) {
		t.Fatalf("白名单外字段应拒绝: %v", err)
	}
}

func TestContentResolverFieldWhitelist(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	res, err := svc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "a1",
		Data: map[string]any{"title": "测试文章", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	r, err := svc.ResolverFor(ctx, "article", res.ID)
	if err != nil {
		t.Fatalf("ResolverFor: %v", err)
	}

	if v, _ := r.ResolveString("article.title"); v != "测试文章" {
		t.Fatalf("title 解析错误: %q", v)
	}

	if v, _ := r.ResolveString("article.excerpt"); v != "摘要" {
		t.Fatalf("excerpt 解析错误: %q", v)
	}

	if v, _ := r.ResolveString("article.body"); v != "" {
		t.Fatalf("未设置字段应空串: %q", v)
	}

	if _, err := r.ResolveString("article.evil"); err == nil {
		t.Fatalf("白名单外字段应拒绝")
	}
}

func TestFieldWhitelistContract(t *testing.T) {
	if !contentcontract.IsValidType("article") {
		t.Fatalf("article 应为合法内容类型")
	}

	for _, gone := range []string{"product", "category", "tag"} {
		if contentcontract.IsValidType(gone) {
			t.Fatalf("类型 %q 已摘除，不应再合法", gone)
		}
	}
	if !contentcontract.IsValidField("article", "title") || contentcontract.IsValidField("article", "price") {
		t.Fatalf("字段白名单错误：article 有 title、无 price")
	}
}
