package feature

// menu_input_fence_test.go — 菜单译文失效的「实体输入版本」栅栏（真实 PG + 真实发布链）。
//
// 三组断言，全部是行为而不是 SQL 字符串：
//  1. 菜单译文变更（MarkInstanceMenuStaleTx）命中声明了 menu:{projectID}:{kind} 依赖的
//     实例，并让实体 input_version +1、stale=true；别工程与无关 kind 不动；
//  2. 该变更之后用**旧输入**激活，被 ErrInputVersionMoved 拒绝，指针不被覆盖、
//     stale 不被假清回 false；
//  3. 按新输入重新冻结再发布可以正常激活并清 stale —— 与 2 成对，证明拒绝的是
//     「旧输入」而不是「永远拒绝」。
//
// 为什么在 feature 层：注入点与「旧产物晚到」是链路事实，需要真实 DDL、迁移 699 的
// input_version 列与真实激活谓词；model 单测只能钉形态。

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

	"go_wp/internal/builder/core"
	"go_wp/internal/pipeline"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// menuFenceDoc 绑定页眉导航位置的模板文档（编译期登记 menu:{projectID}:header）。
const menuFenceDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav1","type":"core.nav","props":{"menu":"header"}}]}`

// menuFenceFixture 一个工程 + 一个已发布、绑定页眉菜单的实例（真实生产迁移建表）。
type menuFenceFixture struct {
	db        *gorm.DB
	model     *presentationmodel.Model
	pres      *presentationservice.Service
	projectID string
	instance  string
	entity    string
}

// newMenuFenceFixture 建表（生产迁移）→ 工程 → 模板 → 导航项 → 实体 → 实例。
//
// 导航项先建：模板里的 core.nav 在编译期按位置读取导航，没有位置数据也能编译，
// 但有数据才与生产形态一致。PG 不可用时 t.Skip（返回 nil），不用 mock 掩盖。
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
	// 导航服务与**真实写端口**：菜单/菜单项 CRUD 的失效派发要求五个端口齐备
	//（fail closed），只给 Instances 一条会让 Update 直接报"写服务未就绪"。
	// 这里与 internal/routers 的 navigationWritePorts 同一组真实实现（page / build 用真实服务）。
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

// newMenuFenceProject 在**另一个工程**里建一份同形实例（同样声明 menu:{otherPID}:header）。
//
// 为什么必须建真实实例而不是复用：没有声明依赖的实例"没被标"什么也证明不了 —— 命中口径
// 天然排除了它。这里要证明的是"键带了本工程 ID"，两个工程各有一条 header 依赖。
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
	// 模板按实体类型取默认：内容模板是工程内资源，这里用独立实体类型避免与主工程串用。
	if _, err := tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "邻工程模板", DraftDocument: []byte(menuFenceDoc),
	}); err != nil {
		t.Fatalf("创建邻工程模板失败: %v", err)
	}
	entity, err := contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "other-fence", Data: map[string]any{"title": "邻工程实体"},
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

// readInstanceFence 读实例行的权威事实（input_version / stale）。
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

// markMenuStaleInTx 以真实事务调用生产入口（不自造 UPDATE）。
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

// TestMenuStaleAdvancesInputVersionAndHitsOnlyDeclaringInstances
// 命中口径：只标声明了该菜单位置依赖的实例，别的 kind 一条不动；版本同语句 +1。
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

	// 无关位置：命中 0 行且版本不动（不许"标了全站"）。
	if n := markMenuStaleInTx(t, f, presentationmodel.MenuStaleKindFooter); n != 0 {
		t.Fatalf("页脚位置未声明依赖，应命中 0 行，实际 %d", n)
	}
	if v, _ := readInstanceFence(t, f.db, f.instance); v != before {
		t.Fatalf("无关位置不得推进版本：before=%d after=%d", before, v)
	}

	// 命中位置：只标本工程声明的实例 —— 另建一个工程，它的实例版本不得被动。
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

// TestStaleCapturedVersionCannotActivateAfterMenuChange
// 旧输入捕获的版本在菜单译文失效后不能激活：拒绝、指针不覆盖、stale 不假清。
func TestStaleCapturedVersionCannotActivateAfterMenuChange(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// ① 旧批次的捕获点：此刻实例行的 input_version 就是「旧批次编译时用的那一代」。
	captured, _ := readInstanceFence(t, f.db, f.instance)
	// 旧批次的产物在第一次发布时就已经落库（同一批字节命中 hash 复用），这里先记下它，
	// 后面要证明的是「旧批次拿不到新版本」，而不是「它不许存在」。
	// 默认语言取实例行的事实（不在此硬编码语言码）。
	var defaultLang string
	if err := f.db.Raw(`SELECT COALESCE(settings->>'defaultLang', settings->>'default_lang', '')
		FROM projects WHERE id = ?`, f.projectID).Scan(&defaultLang).Error; err != nil {
		t.Fatalf("读取工程默认语言失败: %v", err)
	}
	var oldArtifact string
	if err := f.db.Raw(`SELECT COALESCE(artifact_id::text,'') FROM presentation_publications
		WHERE presentation_id = ? AND lang = ?`, f.instance, defaultLang).Scan(&oldArtifact).Error; err != nil {
		t.Fatalf("读取旧语言账本失败: %v", err)
	}

	// ② 冻结之后才发生菜单译文变更：这正是"旧产物晚到"的窗口。
	if n := markMenuStaleInTx(t, f, presentationmodel.MenuStaleKindHeader); n != 1 {
		t.Fatalf("页眉位置应命中 1 个实例，实际 %d", n)
	}
	versionNow, staleNow := readInstanceFence(t, f.db, f.instance)
	if versionNow != captured+1 || !staleNow {
		t.Fatalf("菜单变更后应版本 +1 且 stale：captured=%d now=%d stale=%v", captured, versionNow, staleNow)
	}

	// ③ 新版本先至：按新输入重建一次（真实链路），线上升级到新产物。
	if _, rerr := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{
		ProjectID: f.projectID, EntityID: f.entity,
	}); rerr != nil {
		t.Fatalf("菜单变更后重建应成功: %v", rerr)
	}
	newVersion, staleAfterRebuild := readInstanceFence(t, f.db, f.instance)
	if staleAfterRebuild {
		t.Fatalf("重建成功后 stale 应被清回 false")
	}

	// ④ 旧批次此刻才带着它捕获的版本回来落库：必须被谓词拒绝，且不得改任何一列。
	locked, err := f.model.GetInstance(ctx, f.projectID, f.instance)
	if err != nil {
		t.Fatalf("读取实例失败: %v", err)
	}
	oldActive := ""
	if locked.ActiveArtifactID != nil {
		oldActive = *locked.ActiveArtifactID
	}
	if oldArtifact == "" || oldActive == oldArtifact {
		t.Fatalf("前置不成立：旧产物 %q 必须存在且已不是当前线上产物（当前 %q）", oldArtifact, oldActive)
	}
	// 旧批次的快照 / 指针：它要落的是自己那一代，而不是刚成功的这一代。
	locked.ActiveArtifactID = &oldArtifact
	locked.StagedArtifactID = &oldArtifact
	locked.Stale = false // 旧批次自以为可以清 stale
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
	if activeAfter == oldArtifact {
		t.Fatalf("旧产物不得覆盖新版本指针：old=%q now=%q", oldArtifact, activeAfter)
	}
	if activeAfter != oldActive {
		t.Fatalf("指针应停留在新版本产物：期望 %q 实际 %q", oldActive, activeAfter)
	}
}

// TestRebuildAfterMenuChangeActivatesWithFreshVersion
// 与上一条成对：按新输入重新冻结后发布能正常清 stale —— 拒绝的是旧输入，不是永远拒绝。
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
