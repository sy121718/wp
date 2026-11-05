package feature

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	contentdto "go_wp/internal/module/content/dto"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/pkg/builder/core"
	"go_wp/internal/pipeline"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const menuFenceDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav1","type":"core.nav","props":{"menu":"header"}}]}`

type menuFenceFixture struct {
	db        *gorm.DB
	model     *presentationmodel.Model
	pres      *presentationservice.Service
	projectID string
	instance  string
	entity    string
}

func newMenuFenceFixture(t *testing.T) *menuFenceFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "菜单输入栅栏工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	contentSvc := contentservice.NewService(contentmodel.NewModel(db))
	registry := core.NewEntitySourceRegistry()
	if err := contentSvc.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册实体类型失败: %v", err)
	}
	tplSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, registry)
	pubSvc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	var routes pubcontract.PublicationService = pubSvc
	presSvc := presentationservice.NewService(presentationmodel.NewModel(db), tplSvc, registry, projects, nil, routes)

	navSvc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	presSvc.SetNavigationService(navSvc)
	navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil),
		Instances:     presSvc,
		Items:         pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil),
		ItemInstances: presSvc,
		Queue:         buildservice.NewService(buildmodel.NewModel(db)),
	})

	if _, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: project.ID, Title: "栅栏菜单", Path: "/fence-menu", Kind: "header",
	}); err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}
	if _, err := tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "栅栏模板", DraftDocument: []byte(menuFenceDoc),
	}); err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
	entity, err := contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "menu-fence", Data: map[string]any{"title": "栅栏实体"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := presSvc.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: project.ID, EntityType: "article", EntityID: entity.ID, URLPath: "/menu-fence",
	})
	if err != nil {
		t.Fatalf("创建实例失败: %v", err)
	}
	return &menuFenceFixture{
		db: db, model: presentationmodel.NewModel(db), pres: presSvc,
		projectID: project.ID, instance: inst.ID, entity: entity.ID,
	}
}

func newMenuFenceProject(t *testing.T, f *menuFenceFixture) string {
	t.Helper()
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "菜单输入栅栏邻工程"})
	if err != nil {
		t.Fatalf("创建邻工程失败: %v", err)
	}
	contentSvc := contentservice.NewService(contentmodel.NewModel(f.db))
	registry := core.NewEntitySourceRegistry()
	if err := contentSvc.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册实体类型失败: %v", err)
	}
	tplSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(f.db), projects, registry)
	pubSvc := pubservice.NewService(pubmodel.NewPublicationModel(f.db))
	var routes pubcontract.PublicationService = pubSvc
	presSvc := presentationservice.NewService(presentationmodel.NewModel(f.db), tplSvc, registry, projects, nil, routes)
	navSvc := navigationservice.NewService(navigationmodel.NewModel(f.db), projects)
	presSvc.SetNavigationService(navSvc)
	navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         pageservice.NewService(pagemodel.NewPageModel(f.db), nil, nil, projects, nil, nil, nil, nil, nil),
		Instances:     presSvc,
		Items:         pageservice.NewService(pagemodel.NewPageModel(f.db), nil, nil, projects, nil, nil, nil, nil, nil),
		ItemInstances: presSvc,
		Queue:         buildservice.NewService(buildmodel.NewModel(f.db)),
	})
	if _, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: project.ID, Title: "邻工程菜单", Path: "/other-menu", Kind: "header",
	}); err != nil {
		t.Fatalf("创建邻工程导航项失败: %v", err)
	}

	if _, err := tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		ProjectID: project.ID, EntityType: "article", Name: "邻工程模板", DraftDocument: []byte(menuFenceDoc),
	}); err != nil {
		t.Fatalf("创建邻工程模板失败: %v", err)
	}

	entity, err := contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "other-fence",
		Data: map[string]any{"title": "邻工程实体"},
	})
	if err != nil {
		t.Fatalf("创建邻工程实体失败: %v", err)
	}
	inst, err := presSvc.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: project.ID, EntityType: "article", EntityID: entity.ID, URLPath: "/other-fence",
	})
	if err != nil {
		t.Fatalf("创建邻工程实例失败: %v", err)
	}
	return inst.ID
}

func readInstanceFence(t *testing.T, db *gorm.DB, id string) (version int64, stale bool) {
	t.Helper()
	var row struct {
		InputVersion int64
		Stale        bool
	}
	if err := db.Raw("SELECT input_version, stale FROM presentation_instances WHERE id = ?", id).
		Scan(&row).Error; err != nil {
		t.Fatalf("读取实例版本失败: %v", err)
	}
	return row.InputVersion, row.Stale
}

func markMenuStaleInTx(t *testing.T, f *menuFenceFixture, kind string) int64 {
	t.Helper()
	var marked int64
	err := f.model.Transaction(context.Background(), func(tx *gorm.DB) error {
		n, merr := f.model.MarkInstanceMenuStaleTx(context.Background(), tx, f.projectID, kind, time.Now().UTC())
		marked = n
		return merr
	})
	if err != nil {
		t.Fatalf("MarkInstanceMenuStaleTx 失败: %v", err)
	}
	return marked
}

func TestMenuStaleAdvancesInputVersionAndHitsOnlyDeclaringInstances(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	var deps int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM presentation_dependencies
		WHERE presentation_id = ? AND dependency_kind = ? AND dependency_key = ?`,
		f.instance, pipeline.DepKindMenu, "menu:"+f.projectID+":header").Scan(&deps).Error; err != nil {
		t.Fatalf("统计菜单依赖失败: %v", err)
	}
	if deps != 1 {
		t.Fatalf("绑定页眉菜单的实例应声明恰好 1 条 menu 依赖，实际 %d", deps)
	}
	before, staleBefore := readInstanceFence(t, f.db, f.instance)
	if staleBefore {
		t.Fatalf("刚发布成功的实例不应是 stale")
	}

	if n := markMenuStaleInTx(t, f, presentationmodel.MenuStaleKindFooter); n != 0 {
		t.Fatalf("页脚位置未声明依赖，应命中 0 行，实际 %d", n)
	}
	if v, _ := readInstanceFence(t, f.db, f.instance); v != before {
		t.Fatalf("无关位置不得推进版本：before=%d after=%d", before, v)
	}

	other := newMenuFenceProject(t, f)
	otherBefore, _ := readInstanceFence(t, f.db, other)
	if n := markMenuStaleInTx(t, f, presentationmodel.MenuStaleKindHeader); n != 1 {
		t.Fatalf("页眉位置应命中本工程 1 个实例，实际 %d", n)
	}
	if v, s := readInstanceFence(t, f.db, other); v != otherBefore || s {
		t.Fatalf("别工程的实例不得被标记：version %d→%d stale=%v", otherBefore, v, s)
	}
	after, staleAfter := readInstanceFence(t, f.db, f.instance)
	if after != before+1 {
		t.Fatalf("菜单失效必须把 input_version 原子 +1：before=%d after=%d", before, after)
	}
	if !staleAfter {
		t.Fatalf("菜单失效必须把实例标为 stale")
	}
}

func TestStaleCapturedVersionCannotActivateAfterMenuChange(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	captured, _ := readInstanceFence(t, f.db, f.instance)

	var oldArtifact string
	if err := f.db.Raw(`SELECT COALESCE(MAX(artifact_id::text),'') FROM presentation_publications
		WHERE presentation_id = ?`, f.instance).Scan(&oldArtifact).Error; err != nil {
		t.Fatalf("读取旧语言账本失败: %v", err)
	}
	if oldArtifact == "" {
		t.Fatalf("前置不成立：首次发布后账本里应有当前产物 id")
	}

	if n := markMenuStaleInTx(t, f, presentationmodel.MenuStaleKindHeader); n != 1 {
		t.Fatalf("页眉位置应命中 1 个实例，实际 %d", n)
	}
	versionNow, staleNow := readInstanceFence(t, f.db, f.instance)
	if versionNow != captured+1 || !staleNow {
		t.Fatalf("菜单变更后应版本 +1 且 stale：captured=%d now=%d stale=%v", captured, versionNow, staleNow)
	}

	if _, rerr := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{
		ProjectID: f.projectID, EntityID: f.entity,
	}); rerr != nil {
		t.Fatalf("菜单变更后重建应成功: %v", rerr)
	}
	newVersion, staleAfterRebuild := readInstanceFence(t, f.db, f.instance)
	if staleAfterRebuild {
		t.Fatalf("重建成功后 stale 应被清回 false")
	}

	locked, err := f.model.GetInstance(ctx, f.projectID, f.instance)
	if err != nil {
		t.Fatalf("读取实例失败: %v", err)
	}
	currentActive := ""
	if locked.ActiveArtifactID != nil {
		currentActive = *locked.ActiveArtifactID
	}
	if currentActive == "" {
		t.Fatalf("前置不成立：重建后实例应有在线产物")
	}

	locked.ActiveArtifactID = &oldArtifact
	locked.StagedArtifactID = &oldArtifact
	locked.Stale = false
	err = f.model.Transaction(ctx, func(tx *gorm.DB) error {
		return f.model.WriteInstanceActivationTx(tx, f.projectID, locked, captured)
	})
	if !errors.Is(err, presentationmodel.ErrInputVersionMoved) {
		t.Fatalf("旧版本激活必须被拒（ErrInputVersionMoved），实际 %v", err)
	}
	versionAfter, staleAfter := readInstanceFence(t, f.db, f.instance)
	if versionAfter != newVersion {
		t.Fatalf("被拒的激活不得改版本：期望 %d 实际 %d", newVersion, versionAfter)
	}
	if staleAfter {
		t.Fatalf("被拒的激活不得把新版本先至后的状态改写成 stale")
	}
	var activeAfter string
	if err := f.db.Raw("SELECT COALESCE(active_artifact_id::text,'') FROM presentation_instances WHERE id = ?", f.instance).
		Scan(&activeAfter).Error; err != nil {
		t.Fatalf("读取 active 指针失败: %v", err)
	}

	if activeAfter != currentActive {
		t.Fatalf("指针应停留在新版本产物：期望 %q 实际 %q", currentActive, activeAfter)
	}
}

func TestRebuildAfterMenuChangeActivatesWithFreshVersion(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	before, _ := readInstanceFence(t, f.db, f.instance)
	markMenuStaleInTx(t, f, presentationmodel.MenuStaleKindHeader)

	if _, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{
		ProjectID: f.projectID, EntityID: f.entity,
	}); err != nil {
		t.Fatalf("菜单变更后重建应成功: %v", err)
	}
	after, stale := readInstanceFence(t, f.db, f.instance)
	if after != before+1 {
		t.Fatalf("重建不是输入变化，不得再推进版本：before=%d after=%d", before, after)
	}
	if stale {
		t.Fatalf("重建成功后 stale 应被清回 false")
	}
}
