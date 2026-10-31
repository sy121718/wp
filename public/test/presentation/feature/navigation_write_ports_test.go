package feature

// navigation_write_ports_test.go — 导航写路径的**真实五端口**装配（不 noop、不跨包 helper）。
//
// 为什么必须有这一组：装配层 navigationWritePorts 里 5 个端口的任一条走空实现，
// 症状都是"菜单/菜单项保存成功、线上仍停在旧内容"，而且只在下一次构建才看得出来。
// 之前的用例只覆盖了 Instances 一条（menu_input_fence_test.go 的裸 navSvc.Create），
// 其余四条（Pages / Items / ItemInstances / Queue）从未被真实服务驱动过。
//
// 本文件的口径：
//   - 五个端口全部接**真实模块服务**（page / presentation / build），同一 db、同一工程；
//   - 不用 noop、不用 cross-package helper、不在测试里另写一套适配器；
//   - 断言分成"同事务写了什么"（stale / input_version / 队列行）与"零命中合法"两类，
//     每条同时给出反例（未声明依赖的实体必须原样不动）。

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

	"go_wp/internal/builder/core"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// byItemDoc 按**具体菜单项**引用导航的模板（core.nav 的 props.navigation 分支）。
//
// 与按位置的 menuFenceDoc 不同：构建期只记 navigation:{itemID}，不会被位置键命中。
// 目前只在 skip 分支说明里被引用 —— 模板按实体类型取，同类型建不出第二份。
const byItemDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav1","type":"core.nav","props":{"navigation":"ITEM_ID"}}]}`

// navWriteFixture 一个工程 + 真实五端口 + 两侧真实发布来源（page / presentation）。
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
	instByItem string // 按项实例（模板按实体类型唯一时可能建不出来，见 skip 分支）
}

func newNavWriteFixture(t *testing.T) *navWriteFixture {
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
	// page 服务：只用到它的两个同事务标记端口（MarkMenuStaleTx / MarkNavigationStaleTx），
	// 其余依赖（产物/块/插件/内容/媒体）在这些标记路径上不参与，与 page 侧 unit 测试同形。
	pageSvc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)

	created, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: project.ID, Title: "写端口菜单", Path: "/wp-menu", Kind: "header",
	})
	if err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}
	return &navWriteFixture{
		db: db, projectID: project.ID, navSvc: navSvc, presSvc: presSvc, pageSvc: pageSvc,
		tplSvc: tplSvc, contentSvc: contentSvc, menuItemID: created.ID,
	}
}

// buildSvcTx 用真实 build 服务做 Queue 端口（同一 db，不 noop）。
func (f *navWriteFixture) buildSvcTx() *buildservice.Service {
	if f.buildSvc == nil {
		f.buildSvc = buildservice.NewService(buildmodel.NewModel(f.db))
	}
	return f.buildSvc
}

// menuStaleCount 直接读队列事实（判断"真的入队了"，而不是"端口被调过"）。
func (f *navWriteFixture) menuStaleCount(t *testing.T, kind string) int64 {
	t.Helper()
	var n int64
	// menu_stale 的来源是 "工程:位置"（builddto.MenuStaleSourceID），版本号与去重语境
	// 由迁移 698 的形状约束钉住；这里按 source_type + context_key 数行，不信内存回显。
	// project_id 是小写 uuid 文本（迁移 698 的形状 CHECK 要求 context_key = 前 37 位 + 位置），
	// 所以这里按面值拼，不猜大小写变体。
	key := f.projectID + ":" + kind
	if err := f.db.Raw(`SELECT COUNT(*) FROM build_jobs
		WHERE source_type = 'menu_stale' AND context_key = ?`, key).Scan(&n).Error; err != nil {
		t.Fatalf("统计 menu_stale 任务失败: %v", err)
	}
	return n
}

// TestNavigationWritePortsAreAllReal 五个端口全部真实装配时，一次菜单项更新把两条
// 依赖键（位置 / 项）各自命中，并真的入队。
func TestNavigationWritePortsAreAllReal(t *testing.T) {
	f := newNavWriteFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 按位置引用的实例。
	if _, err := f.tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "位置模板", DraftDocument: []byte(menuFenceDoc),
	}); err != nil {
		t.Fatalf("创建位置模板失败: %v", err)
	}
	positionEntity, err := f.contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "nav-wp-position", Data: map[string]any{"title": "位置实体"},
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
	// 按项引用的实例：模板按**实体类型**唯一，所以换一个实体类型（product）单独建一份
	// 只含按项分支的模板。用它而不是"同类型第二份模板"，两条依赖键才能落在两个实例上，
	// 断言也就真的分得开 —— 不需要 Skip 充数。
	itemDoc := []byte(byItemDocWith(f.menuItemID))
	if _, err := f.tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "product", Name: "按项模板", DraftDocument: itemDoc,
	}); err != nil {
		t.Fatalf("创建按项模板失败: %v", err)
	}
	itemEntity, err := f.contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "product", Slug: "nav-wp-item", Data: map[string]any{"title": "按项实体"},
	})
	if err != nil {
		t.Fatalf("创建按项实体失败: %v", err)
	}
	byItem, err := f.presSvc.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: itemEntity.ID, URLPath: "/nav-wp-item",
	})
	if err != nil {
		t.Fatalf("创建按项实例失败: %v", err)
	}
	f.instByPos, f.instByItem = byPos.ID, byItem.ID

	// 真实五端口（与 internal/routers 的 navigationWritePorts 同一组实现，只是这里直接给）。
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

	// 位置键命中的实例被标 stale 且版本推进。
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
	// 按项键命中的实例同样被标 stale（这就是 ItemInstances 端口存在的理由）。
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
	// Queue 端口必须真的落了一条 menu_stale（kind 是位置名）。
	if n := f.menuStaleCount(t, "header"); n == 0 {
		t.Fatalf("菜单项更新应在真实 build 队列里留下 header 的 menu_stale 任务")
	}
}

// TestNavigationWritePortsFailClosed 缺任一端口时整笔拒绝，且**不产生任何持久副作用**。
//
// 这条是"没接就等于坏了"的判据：装配缺端口时若照常提交，表现是保存成功、线上不变。
func TestNavigationWritePortsFailClosed(t *testing.T) {
	f := newNavWriteFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         f.pageSvc,
		Instances:     f.presSvc,
		Items:         f.pageSvc,
		ItemInstances: nil, // 故意缺按项实例端口
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
	if n := f.menuStaleCount(t, "header"); n != 0 {
		t.Fatalf("拒绝的写不能入队，实际 %d 条", n)
	}
}

// TestNavigationItemStaleDoesNotTouchUnrelatedInstances 按项失效的命中口径：
// 只标声明了该 navigation:{id} 的实例，未声明的原样不动。
func TestNavigationItemStaleDoesNotTouchUnrelatedInstances(t *testing.T) {
	f := newNavWriteFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 未声明任何导航依赖的实例（模板只放一个标题块）。
	plainDoc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err := f.tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "无导航模板", DraftDocument: []byte(plainDoc),
	}); err != nil {
		t.Fatalf("创建无导航模板失败: %v", err)
	}
	entity, err := f.contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "nav-wp-plain", Data: map[string]any{"title": "无关实体"},
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
	// 在同一事务里直接调端口（不经 CRUD），断言命中的集合精确为零。
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

// byItemDocWith 把具体菜单项 id 填进按项模板（编译期按项引用）。
func byItemDocWith(id string) string {
	return strings.Replace(byItemDoc, "ITEM_ID", id, 1)
}
