package feature

import (
	"context"
	"strings"
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
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

const byItemDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav1","type":"core.nav","props":{"navigation":"ITEM_ID"}}]}`

type navWriteFixture struct {
	db         *gorm.DB
	projectID  string
	navSvc     *navigationservice.Service
	presSvc    *presentationservice.Service
	pageSvc    *pageservice.Service
	buildSvc   *buildservice.Service
	tplSvc     *contenttemplateservice.Service
	contentSvc *contentservice.Service
	menuItemID string
	instByPos  string
	instByItem string
}

func newNavWriteFixture(t *testing.T) *navWriteFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "导航写端口工程"})
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

	pageSvc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)

	fixture := &navWriteFixture{
		db: db, projectID: project.ID, navSvc: navSvc, presSvc: presSvc, pageSvc: pageSvc,
		tplSvc: tplSvc, contentSvc: contentSvc,
	}
	navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages: fixture.pageSvc, Instances: fixture.presSvc,
		Items: fixture.pageSvc, ItemInstances: fixture.presSvc,
		Queue: fixture.buildSvcTx(),
	})

	created, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: project.ID, Title: "写端口菜单", Path: "/wp-menu", Kind: "header",
	})
	if err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}
	fixture.menuItemID = created.ID
	return fixture
}

func (f *navWriteFixture) buildSvcTx() *buildservice.Service {
	if f.buildSvc == nil {
		f.buildSvc = buildservice.NewService(buildmodel.NewModel(f.db))
	}
	return f.buildSvc
}

func (f *navWriteFixture) menuStaleCount(t *testing.T, kind string) int64 {
	t.Helper()
	var n int64

	key := f.projectID + ":" + kind
	if err := f.db.Raw(`SELECT COUNT(*) FROM build_jobs
		WHERE source_type = 'menu_stale' AND context_key = ?`, key).Scan(&n).Error; err != nil {
		t.Fatalf("统计 menu_stale 任务失败: %v", err)
	}
	return n
}

func TestNavigationWritePortsAreAllReal(t *testing.T) {
	f := newNavWriteFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		ProjectID: f.projectID, EntityType: "article", Name: "位置模板", DraftDocument: []byte(menuFenceDoc),
	}); err != nil {
		t.Fatalf("创建位置模板失败: %v", err)
	}
	positionEntity, err := f.contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "nav-wp-position",
		Data: map[string]any{"title": "位置实体"},
	})
	if err != nil {
		t.Fatalf("创建位置实体失败: %v", err)
	}
	byPos, err := f.presSvc.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: positionEntity.ID, URLPath: "/nav-wp-position",
	})
	if err != nil {
		t.Fatalf("创建位置实例失败: %v", err)
	}

	itemDoc := []byte(byItemDocWith(f.menuItemID))
	itemTpl, err := f.tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		ProjectID: f.projectID, EntityType: "article", Name: "按项模板", DraftDocument: itemDoc,
	})
	if err != nil {
		t.Fatalf("创建按项模板失败: %v", err)
	}
	itemEntity, err := f.contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "nav-wp-item",
		Data: map[string]any{"title": "按项实体"},
	})
	if err != nil {
		t.Fatalf("创建按项实体失败: %v", err)
	}
	byItem, err := f.presSvc.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: itemEntity.ID,
		URLPath: "/nav-wp-item", TemplateID: itemTpl.ID,
	})
	if err != nil {
		t.Fatalf("创建按项实例失败: %v", err)
	}
	f.instByPos, f.instByItem = byPos.ID, byItem.ID

	f.navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         f.pageSvc,
		Instances:     f.presSvc,
		Items:         f.pageSvc,
		ItemInstances: f.presSvc,
		Queue:         f.buildSvcTx(),
	})

	newPath := "/wp-menu-renamed"
	if _, err := f.navSvc.Update(ctx, &navigationdto.UpdateReq{ID: f.menuItemID, Path: &newPath}); err != nil {
		t.Fatalf("更新导航项失败: %v", err)
	}

	var posStale bool
	var posVersion int64
	if err := f.db.Raw(`SELECT stale, input_version FROM presentation_instances WHERE id = ?`,
		byPos.ID).Row().Scan(&posStale, &posVersion); err != nil {
		t.Fatalf("读取位置实例失败: %v", err)
	}
	if !posStale || posVersion == 0 {
		t.Fatalf("声明了 menu:{pid}:header 的实例应被标 stale 且 input_version>0，实际 stale=%v version=%d",
			posStale, posVersion)
	}

	var itemStale bool
	var itemVersion int64
	if err := f.db.Raw(`SELECT stale, input_version FROM presentation_instances WHERE id = ?`,
		byItem.ID).Row().Scan(&itemStale, &itemVersion); err != nil {
		t.Fatalf("读取按项实例失败: %v", err)
	}
	if !itemStale || itemVersion == 0 {
		t.Fatalf("声明了 navigation:{id} 的实例应被标 stale 且 input_version>0，实际 stale=%v version=%d",
			itemStale, itemVersion)
	}

	if n := f.menuStaleCount(t, "header"); n == 0 {
		t.Fatalf("菜单项更新应在真实 build 队列里留下 header 的 menu_stale 任务")
	}
}

func TestNavigationWritePortsFailClosed(t *testing.T) {
	f := newNavWriteFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	queuedBefore := f.menuStaleCount(t, "header")
	f.navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         f.pageSvc,
		Instances:     f.presSvc,
		Items:         f.pageSvc,
		ItemInstances: nil,
		Queue:         f.buildSvcTx(),
	})
	newPath := "/wp-menu-blocked"
	if _, err := f.navSvc.Update(ctx, &navigationdto.UpdateReq{ID: f.menuItemID, Path: &newPath}); err == nil {
		t.Fatalf("缺 ItemInstances 端口时必须整笔拒绝（fail closed）")
	}
	var path string
	if err := f.db.Raw(`SELECT path FROM navigations WHERE id = ?`, f.menuItemID).Scan(&path).Error; err != nil {
		t.Fatalf("读取导航项失败: %v", err)
	}
	if path == newPath {
		t.Fatalf("拒绝的写不能留下副作用（路径仍是 %s）", path)
	}
	if n := f.menuStaleCount(t, "header"); n != queuedBefore {
		t.Fatalf("拒绝的写不能入队：before=%d after=%d", queuedBefore, n)
	}
}

func TestNavigationItemStaleDoesNotTouchUnrelatedInstances(t *testing.T) {
	f := newNavWriteFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	plainDoc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err := f.tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		ProjectID: f.projectID, EntityType: "article", Name: "无导航模板", DraftDocument: []byte(plainDoc),
	}); err != nil {
		t.Fatalf("创建无导航模板失败: %v", err)
	}
	entity, err := f.contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "nav-wp-plain",
		Data: map[string]any{"title": "无关实体"},
	})
	if err != nil {
		t.Fatalf("创建无关实体失败: %v", err)
	}
	plain, err := f.presSvc.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/nav-wp-plain",
	})
	if err != nil {
		t.Fatalf("创建无关实例失败: %v", err)
	}

	var hit int64
	if err := f.db.Transaction(func(tx *gorm.DB) error {
		n, merr := f.presSvc.MarkInstanceNavigationStaleTx(ctx, tx, f.projectID, f.menuItemID, time.Now().UTC())
		hit = n
		return merr
	}); err != nil {
		t.Fatalf("按项标记失败: %v", err)
	}
	if hit != 0 {
		t.Fatalf("没有任何实例声明 navigation:%s，命中必须为 0，实际 %d", f.menuItemID, hit)
	}
	var stale bool
	if err := f.db.Raw(`SELECT stale FROM presentation_instances WHERE id = ?`, plain.ID).Scan(&stale).Error; err != nil {
		t.Fatalf("读取无关实例失败: %v", err)
	}
	if stale {
		t.Fatalf("未声明该菜单项依赖的实例不得被标 stale")
	}
}

func byItemDocWith(id string) string {
	return strings.Replace(byItemDoc, "ITEM_ID", id, 1)
}
