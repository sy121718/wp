package unit

import (
	"context"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	navigationdto "go_wp/internal/module/navigation/dto"

	presentationdto "go_wp/internal/module/presentation/dto"

	"go_wp/internal/pipeline"
)

const presNavDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav1","type":"core.nav","props":{"menu":"header"}}]}`

func TestPresentationMenuDependencyAndStale(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	navSvc := f.newNavService()
	if _, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: f.projectID, Title: "旧菜单", Path: "/old-menu", Kind: "header",
	}); err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}

	f.pres.SetNavigationService(navSvc)

	if _, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "导航模板", DraftDocument: []byte(presNavDocument),
	}); err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "nav-instance", Data: map[string]any{"title": "导航实例"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/nav-instance",
	})
	if err != nil {
		t.Fatalf("创建实例失败: %v", err)
	}

	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM presentation_dependencies
		WHERE presentation_id = ? AND dependency_kind = ? AND dependency_key = ?`,
		inst.ID, pipeline.DepKindMenu, "menu:"+f.projectID+":header").Scan(&n).Error; err != nil {
		t.Fatalf("统计导航依赖失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("绑定页眉导航的实例应声明恰好 1 条 menu:{pid}:header 依赖，实际 %d 条", n)
	}

	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePresentation, f.pres)
	navSvc.SetMenuStaleDispatcher(pipeline.NewMenuStaleAdapter(fanout))
	newPath := "/new-menu"
	var itemID string
	rows, err := navSvc.List(ctx, &navigationdto.ListReq{ProjectID: f.projectID, Kind: "header"})
	if err != nil || len(rows) == 0 {
		t.Fatalf("读取导航项失败: %v", err)
	}
	itemID = rows[0].ID
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, Path: &newPath}); err != nil {
		t.Fatalf("更新导航项失败: %v", err)
	}

	var stale bool
	if err := f.db.Raw(`SELECT stale FROM presentation_instances WHERE id = ?`, inst.ID).Scan(&stale).Error; err != nil {
		t.Fatalf("读取实例 stale 失败: %v", err)
	}
	if !stale {
		t.Fatalf("声明了该导航依赖的实例应被标记待重建")
	}
}
