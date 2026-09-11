// Package unit content 模块 feature 测试（真实 PostgreSQL，0-A2）：
// 内容 CRUD + revision 单调递增 + Resolver 字段白名单解析。
//
// 迁移 080（issue #4）后内容实体只保留 article：商品与分类改由领域模块自己的表承载，
// 本包的用例统一用 article 作为内容类型。
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

// newService 隔离 PG schema + AutoMigrate contents + 装配 service。
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

// TestContentCRUD 内容 CRUD + revision 单调递增。
func TestContentCRUD(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 创建。
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

	// 更新 → revision 递增。
	res2, err := svc.Update(ctx, &contentdto.UpdateReq{
		ID: id, Data: map[string]any{"title": "夏季故事改", "excerpt": "摘要 v2"},
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if res2.Revision != 2 {
		t.Fatalf("revision 应递增为 2: %d", res2.Revision)
	}

	// 删除。
	if err := svc.Delete(ctx, &contentdto.DeleteReq{ID: id}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := svc.Get(ctx, &contentdto.GetReq{ID: id}); err == nil {
		t.Fatalf("删除后应查不到")
	}
}

// TestContentRejectsForeignField 白名单外字段拒绝（不变量 4）。
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

// TestContentResolverFieldWhitelist Resolver 按白名单解析字段值。
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
	// 字符串字段。
	if v, _ := r.ResolveString("article.title"); v != "测试文章" {
		t.Fatalf("title 解析错误: %q", v)
	}
	// 数组字段取首元素（featuredImage 声明为单值，这里用 body 富文本回归纯字符串）。
	if v, _ := r.ResolveString("article.excerpt"); v != "摘要" {
		t.Fatalf("excerpt 解析错误: %q", v)
	}
	// 未设置字段 → 空串。
	if v, _ := r.ResolveString("article.body"); v != "" {
		t.Fatalf("未设置字段应空串: %q", v)
	}
	// 白名单外字段 → 错误。
	if _, err := r.ResolveString("article.evil"); err == nil {
		t.Fatalf("白名单外字段应拒绝")
	}
}

// TestFieldWhitelistContract 字段白名单契约：内容类型收敛为 article（迁移 080 / issue #4）。
func TestFieldWhitelistContract(t *testing.T) {
	if !contentcontract.IsValidType("article") {
		t.Fatalf("article 应为合法内容类型")
	}
	// 商品与分类已摘除（改由领域模块自己的表承载），不得再被内容白名单接受。
	for _, gone := range []string{"product", "category", "tag"} {
		if contentcontract.IsValidType(gone) {
			t.Fatalf("类型 %q 已摘除，不应再合法", gone)
		}
	}
	if !contentcontract.IsValidField("article", "title") || contentcontract.IsValidField("article", "price") {
		t.Fatalf("字段白名单错误：article 有 title、无 price")
	}
}
