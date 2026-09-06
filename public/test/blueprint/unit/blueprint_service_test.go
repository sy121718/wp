// Package unit blueprint 模块 feature 测试（真实 PostgreSQL，0-B）：
// Blueprint 创建 → 初始版本、更新 → draft_version 递增、InitPageDocument
// 复制 AST 并递归重写 Node ID、非法 Kind 拒绝。
package unit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	blueprintdto "go_wp/internal/module/blueprint/dto"
	blueprintmodel "go_wp/internal/module/blueprint/model"
	blueprintservice "go_wp/internal/module/blueprint/service"

	"go_wp/public/test/support"
)

// pageDocument 最小合法页面文档（空 root）。
const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// nestedDocument 含嵌套组件树的合法页面文档（container 内嵌 heading），
// 用于验证 InitPageDocument 深度遍历 Children 递归重写 ID。
const nestedDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c1","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}]}`

// newService 隔离 PG schema + AutoMigrate 两张表 + 装配 service。
func newService(t *testing.T) (*blueprintservice.Service, *gorm.DB) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, nil
	}
	if err := db.AutoMigrate(&blueprintmodel.BlueprintEntity{}, &blueprintmodel.VersionEntity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	return blueprintservice.NewService(blueprintmodel.NewModel(db)), db
}

// TestBlueprintCreate 创建 → draft_version=1 且 LatestVersion 存在。
func TestBlueprintCreate(t *testing.T) {
	svc, db := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Create(ctx, &blueprintdto.CreateReq{
		Name:          "首页蓝图",
		Kind:          "home",
		DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if res.DraftVersion != 1 {
		t.Fatalf("初始 draft_version 应为 1: %d", res.DraftVersion)
	}

	// LatestVersion 应立即存在（version=1 快照）。
	ver, err := blueprintmodel.NewModel(db).LatestVersion(ctx, res.ID)
	if err != nil {
		t.Fatalf("LatestVersion 应存在: %v", err)
	}
	if ver.Version != 1 {
		t.Fatalf("初始版本号应为 1: %d", ver.Version)
	}
}

// TestBlueprintUpdate 更新 → draft_version 递增 + 版本数=2。
func TestBlueprintUpdate(t *testing.T) {
	svc, db := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Create(ctx, &blueprintdto.CreateReq{
		Name:          "文章蓝图",
		Kind:          "article",
		DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	res2, err := svc.Update(ctx, &blueprintdto.UpdateReq{
		ID:            res.ID,
		DraftDocument: json.RawMessage(nestedDocument),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if res2.DraftVersion != 2 {
		t.Fatalf("draft_version 应递增为 2: %d", res2.DraftVersion)
	}

	// 版本表应产生 2 条记录。
	var versions []blueprintmodel.VersionEntity
	if err := db.Where("blueprint_id = ?", res.ID).Find(&versions).Error; err != nil {
		t.Fatalf("查询版本失败: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("版本数应为 2: %d", len(versions))
	}
}

// TestBlueprintInitPageDocument 复制 AST 并递归重写全部 Node ID。
func TestBlueprintInitPageDocument(t *testing.T) {
	svc, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Create(ctx, &blueprintdto.CreateReq{
		Name:          "嵌套蓝图",
		Kind:          "page",
		DraftDocument: json.RawMessage(nestedDocument),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	got, err := svc.InitPageDocument(ctx, res.ID)
	if err != nil {
		t.Fatalf("InitPageDocument 失败: %v", err)
	}

	// 解析返回 AST（container 根 + heading 子节点两层）。
	var page struct {
		Root []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Children []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"children"`
		} `json:"root"`
	}
	if err := json.Unmarshal(got, &page); err != nil {
		t.Fatalf("返回 AST 解析失败: %v", err)
	}
	if len(page.Root) != 1 || len(page.Root[0].Children) != 1 {
		t.Fatalf("AST 结构不完整（应为 1 根 + 1 子）: %s", got)
	}

	// 全部 node.ID（含嵌套子节点）应为合法 UUID，且与原文档 ID（c1/h1）不同、互不相同。
	gotIDs := []string{page.Root[0].ID, page.Root[0].Children[0].ID}
	for _, id := range gotIDs {
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("node.ID 应为合法 UUID: %q", id)
		}
		if id == "c1" || id == "h1" {
			t.Fatalf("node.ID 不应保留原 ID: %q", id)
		}
	}
	if page.Root[0].ID == page.Root[0].Children[0].ID {
		t.Fatalf("复制后各 node.ID 应互不相同: %q", page.Root[0].ID)
	}
	if page.Root[0].Type != "core.container" || page.Root[0].Children[0].Type != "core.heading" {
		t.Fatalf("组件类型应原样保留: %s", got)
	}
}

// TestBlueprintRejectInvalidKind 非法 Kind 拒绝。
func TestBlueprintRejectInvalidKind(t *testing.T) {
	svc, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	if _, err := svc.Create(ctx, &blueprintdto.CreateReq{
		Name:          "非法蓝图",
		Kind:          "gallery",
		DraftDocument: json.RawMessage(pageDocument),
	}); err == nil {
		t.Fatalf("非法 Kind 应拒绝")
	}
}
