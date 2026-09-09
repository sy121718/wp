// Package unit content 模块 feature 测试（真实 PostgreSQL，0-A2）：
// 内容 CRUD + revision 单调递增 + Resolver 字段白名单解析。
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
		EntityType: "product", Slug: "summer-shirt",
		Data: map[string]any{"name": "夏季衬衫", "price": 99.0},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if res.Revision != 1 || res.Slug != "summer-shirt" {
		t.Fatalf("创建响应错误: %+v", res)
	}
	id := res.ID

	// 更新 → revision 递增。
	res2, err := svc.Update(ctx, &contentdto.UpdateReq{
		ID: id, Data: map[string]any{"name": "夏季衬衫改", "price": 89.0},
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
		EntityType: "product", Slug: "x",
		Data: map[string]any{"name": "ok", "evil": "injected"},
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
		EntityType: "product", Slug: "p1",
		Data: map[string]any{"name": "测试商品", "price": 199.0, "images": []any{"https://img/a.jpg"}},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	r, err := svc.ResolverFor(ctx, "product", res.ID)
	if err != nil {
		t.Fatalf("ResolverFor: %v", err)
	}
	// 字符串字段。
	if v, _ := r.ResolveString("product.name"); v != "测试商品" {
		t.Fatalf("name 解析错误: %q", v)
	}
	// 数值归一（199.0 → 199）。
	if v, _ := r.ResolveString("product.price"); v != "199" {
		t.Fatalf("price 归一错误: %q", v)
	}
	// 数组取首元素。
	if v, _ := r.ResolveString("product.images"); v != "https://img/a.jpg" {
		t.Fatalf("images 取首错误: %q", v)
	}
	// 未设置字段 → 空串。
	if v, _ := r.ResolveString("product.description"); v != "" {
		t.Fatalf("未设置字段应空串: %q", v)
	}
	// 白名单外字段 → 错误。
	if _, err := r.ResolveString("product.evil"); err == nil {
		t.Fatalf("白名单外字段应拒绝")
	}
}

// TestFieldWhitelistContract 字段白名单契约（contenttemplate 未来校验依赖）。
func TestFieldWhitelistContract(t *testing.T) {
	if !contentcontract.IsValidType("product") || contentcontract.IsValidType("tag") {
		t.Fatalf("类型白名单错误")
	}
	if !contentcontract.IsValidField("article", "title") || contentcontract.IsValidField("article", "price") {
		t.Fatalf("字段白名单错误：article 无 price")
	}
	if len(contentcontract.FieldWhitelist("category")) != 3 {
		t.Fatalf("category 字段数错误")
	}
}
