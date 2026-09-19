package unit

// presentation_menu_dependency_test.go — 导航变更的失效传播（presentation 来源）。
//
// 与 page 侧成对：改公开站点导航后，把该菜单位置烘进产物的**自动发布实例**同样要重建。
// 两半：
//   登记：模板里的 core.nav 绑定了 menu=header 时，构建期把 menu:{projectID}:header
//         写进 presentation_dependencies（presentation_render.go 的 presentationDependencies）；
//   触发：navigation 写操作经 pipeline.MenuStaleAdapter → Fanout → 实例被标 stale。
//
// 键必须有工程 ID：导航是工程级资源，位置名只有 header/footer，不带工程会跨工程误标
//（page 侧的同名用例覆盖那条断言）。

import (
	"context"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	presentationdto "go_wp/internal/module/presentation/dto"

	"go_wp/internal/pipeline"
)

// presNavDocument 绑定页眉导航位置的模板文档（无手写 items）。
const presNavDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav1","type":"core.nav","props":{"menu":"header"}}]}`

// TestPresentationMenuDependencyAndStale 实例登记 menu 依赖，且改导航后被标 stale。
func TestPresentationMenuDependencyAndStale(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 导航服务：projects 传 nil（服务内回退 projects 表清单，只用于逐工程定位）。
	navSvc := navigationservice.NewService(navigationmodel.NewModel(f.db), nil)
	if _, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: f.projectID, Title: "旧菜单", Path: "/old-menu", Kind: "header",
	}); err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}
	// 与真实装配同形：实例侧的站点装配注入导航解析器。
	f.pres.SetNavigationService(navSvc)

	// 模板绑定页眉导航位置。
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

	// 1) 登记：实例的活跃产物声明了 menu:{projectID}:header。
	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM presentation_dependencies
		WHERE presentation_id = ? AND dependency_kind = ? AND dependency_key = ?`,
		inst.ID, pipeline.DepKindMenu, "menu:"+f.projectID+":header").Scan(&n).Error; err != nil {
		t.Fatalf("统计导航依赖失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("绑定页眉导航的实例应声明恰好 1 条 menu:{pid}:header 依赖，实际 %d 条", n)
	}

	// 2) 触发：扇出注册 presentation 来源，导航写操作经适配器反查依赖表。
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
