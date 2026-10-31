package rlstest

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
)

func seedNavigation(t *testing.T, db *gorm.DB, projectID, title, path string) string {
	t.Helper()
	id := uuid.NewString()
	err := navigationmodel.NewModel(db).Create(context.Background(), &navigationmodel.NavigationEntity{
		ID: id, ProjectID: projectID, Title: title, Path: path,
		Kind: "header", SourceType: "custom", Target: "self",
		CreatedAt: timeNow(), UpdatedAt: timeNow(),
	})
	if err != nil {
		t.Fatalf("写入导航项失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return id
}

func attachNavigationWritePorts(t *testing.T, svc *navigationservice.Service,
	db *gorm.DB, projects projectcontract.ProjectService) {
	t.Helper()
	if svc == nil || db == nil {
		t.Fatal("装写端口需要真实的导航服务与同一作用域的库句柄")
	}
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	pages := pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects,
		nil, nil, nil, nil, nil)
	instances := presentationservice.NewService(presentationmodel.NewModel(db),
		nil, nil, projects, nil, routes)
	queue := buildservice.NewService(buildmodel.NewModel(db))
	svc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         pages,
		Instances:     instances,
		Items:         pages,
		ItemInstances: instances,
		Queue:         queue,
	})
}

func navigationScopeFixture(t *testing.T) (*gorm.DB, *navigationservice.Service, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	attachNavigationWritePorts(t, svc, db, projects)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, svc, pA, pB
}

func TestRLS_NavigationLocate_NoScopeFailsClosed(t *testing.T) {
	db, _, pA, pB := navigationScopeFixture(t)
	ctx := context.Background()
	idB := seedNavigation(t, db, pB, "B 的菜单", "/b")

	var raw int64
	if err := db.Table("navigations").Where("id = ?", idB).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}

	if _, err := navigationmodel.NewModel(db).Get(ctx, "", idB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("缺作用域按 id 直查应 ErrRecordNotFound（fail closed），实际 %v", err)
	}

	if _, err := navigationmodel.NewModel(db).Get(ctx, pA, idB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿工程 A 的作用域读工程 B 的导航项应 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := navigationmodel.NewModel(db).Get(ctx, pB, idB); err != nil {
		t.Fatalf("拿工程 B 的作用域读本工程导航项应成功，实际 %v", err)
	}
}

func TestRLS_NavigationLocate_ServiceReadsAcrossProjects(t *testing.T) {
	db, svc, pA, pB := navigationScopeFixture(t)
	ctx := context.Background()
	idA := seedNavigation(t, db, pA, "A 的菜单", "/a")
	idB := seedNavigation(t, db, pB, "B 的菜单", "/b")

	res, err := svc.Get(ctx, &navigationdto.GetReq{ID: idB})
	if err != nil {
		t.Fatalf("多工程下按 id 取详情应成功（逐工程定位）: %v", err)
	}
	if res.ProjectID != pB {
		t.Fatalf("应定位到工程 B，实际 %s", res.ProjectID)
	}
	if res2, err := svc.Get(ctx, &navigationdto.GetReq{ID: idA}); err != nil || res2.ProjectID != pA {
		t.Fatalf("应定位到工程 A，实际 %+v（err=%v）", res2, err)
	}

	if _, err := svc.Get(ctx, &navigationdto.GetReq{ID: uuid.NewString()}); err == nil ||
		err.Error() != navigationenums.ErrNotFound {
		t.Fatalf("不存在的导航项应 ErrNotFound，实际 %v", err)
	}
}

func TestRLS_NavigationLocate_ServiceWritesAcrossProjects(t *testing.T) {
	db, svc, _, pB := navigationScopeFixture(t)
	ctx := context.Background()
	idB := seedNavigation(t, db, pB, "B 的菜单", "/b-orig")

	title := "B 的菜单（改后）"
	if _, err := svc.Update(ctx, &navigationdto.UpdateReq{ID: idB, Title: &title}); err != nil {
		t.Fatalf("多工程下只带 id 更新应成功（逐工程定位 + 事务作用域）: %v", err)
	}
	e, err := navigationmodel.NewModel(db).Get(ctx, pB, idB)
	if err != nil {
		t.Fatalf("回读更新后的导航项失败: %v", err)
	}
	if e.Title != title {
		t.Fatalf("标题应已更新为 %q，实际 %q —— 更新静默落到 0 行", title, e.Title)
	}

	if err := svc.Delete(ctx, &navigationdto.DeleteReq{ID: idB}); err != nil {
		t.Fatalf("多工程下只带 id 删除应成功: %v", err)
	}
	if _, err := navigationmodel.NewModel(db).Get(ctx, pB, idB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("删除后应查不到，实际 %v", err)
	}
}

func TestRLS_NavigationLocate_EmptyProjectTableFailsExplicitly(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	svc := navigationservice.NewService(navigationmodel.NewModel(db),
		projectservice.NewService(projectmodel.NewProjectModel(db)))

	_, err := svc.Get(context.Background(), &navigationdto.GetReq{ID: uuid.NewString()})
	if err == nil || err.Error() != navigationenums.ErrProjectRequired {
		t.Fatalf("无工程时定位应显式失败 ErrProjectRequired（而不是静默 ErrNotFound），实际 %v", err)
	}
}
