package feature

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/pkg/facing"

	"go_wp/public/test/support"
)

// 这一组用例验的是导航批量写的「整批原子性」：
// 所有失败见证都来自真实事务/SQL 状态，不靠猜 DELETE ... IN 的行顺序。

func addNavItemKind(t *testing.T, svc navigationcontract.NavigationService, projectID, kind, title, path string, parent *string) string {
	t.Helper()
	res, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: path, Kind: kind, ParentID: parent,
	})
	if err != nil {
		t.Fatalf("创建导航项 %s/%s 失败: %v", kind, title, err)
	}
	return res.ID
}

func navExists(t *testing.T, db *gorm.DB, id string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM navigations WHERE id = ?`, id).Scan(&n).Error; err != nil {
		t.Fatalf("统计导航项 %s 失败: %v", id, err)
	}
	return n > 0
}

type failMenuQueue struct {
	nav  navigationcontract.MenuStaleEnqueuePort
	kind string
}

func (f failMenuQueue) EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (int64, error) {
	if kind == f.kind {
		return 0, errors.New("注入的队列失效故障")
	}
	return f.nav.EnqueueMenuStaleTx(ctx, tx, projectID, kind)
}

func attachFailQueue(t *testing.T, svc navigationcontract.NavigationService, fx navWritePortFixture, kind string) {
	t.Helper()
	setter, ok := svc.(interface {
		SetWritePorts(navigationservice.NavigationWriteDeps)
	})
	if !ok {
		t.Fatal("NavSvc 未暴露 SetWritePorts")
	}
	setter.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         fx.Pages,
		Instances:     fx.Instances,
		Items:         fx.Items,
		ItemInstances: fx.ItemInstances,
		Queue:         failMenuQueue{nav: fx.Queue, kind: kind},
	})
}

// TestNavigationBulkDeleteMixedKindSubtreeIsAtomic 覆盖：
// 同一批里页眉与页脚两棵子树都删净，且每个实际被删的行（含子孙）都按项失效，
// 每个涉及的 kind 都标记菜单失效。
func TestNavigationBulkDeleteMixedKindSubtreeIsAtomic(t *testing.T) {
	db, _, navSvc, projectID, ports := newNavigationEnvPorts(t)
	counter := &navStaleCounter{}
	navSwapAttachCounter(t, navSvc, ports, counter)
	ctx := context.Background()

	headerRoot := addNavItemKind(t, navSvc, projectID, "header", "页眉根", "/h-root", nil)
	headerChild := addNavItemKind(t, navSvc, projectID, "header", "页眉子", "/h-child", &headerRoot)
	headerGrand := addNavItemKind(t, navSvc, projectID, "header", "页眉孙", "/h-grand", &headerChild)
	footerRoot := addNavItemKind(t, navSvc, projectID, "footer", "页脚根", "/f-root", nil)
	footerChild := addNavItemKind(t, navSvc, projectID, "footer", "页脚子", "/f-child", &footerRoot)
	keep := addNavItemKind(t, navSvc, projectID, "header", "旁支", "/keep", nil)

	baseMenu, baseNav := counter.counts()
	baseMenus, baseItems := counter.snapshot()

	if err := navSvc.BulkDelete(ctx, &navigationdto.BulkDeleteReq{
		IDs: []string{headerRoot, footerRoot},
	}); err != nil {
		t.Fatalf("合法混合 kind 批量删除应成功: %v", err)
	}

	for _, id := range []string{headerRoot, headerChild, headerGrand, footerRoot, footerChild} {
		if navExists(t, db, id) {
			t.Fatalf("子树中的行 %s 应被删除", id)
		}
	}
	if !navExists(t, db, keep) {
		t.Fatal("未参与批量的旁支行不该被删除")
	}

	menu, nav := counter.counts()
	if menu-baseMenu != 2 {
		t.Fatalf("两个 kind 各标记一次菜单失效，实际新增 %d 次（%v → %v）",
			menu-baseMenu, baseMenus, mustMenuKeys(counter))
	}
	if nav-baseNav != 5 {
		t.Fatalf("5 个实际被删的行都应按项失效，实际新增 %d 次（%v → %v）",
			nav-baseNav, baseItems, mustNavKeys(counter))
	}
	// 只比较本步新增的键：建项本身也会派发按项失效，基线里已含既有键。
	itemKeys := mustNavKeys(counter)
	newKeys := append([]string(nil), itemKeys[len(baseItems):]...)
	for _, id := range []string{headerRoot, headerChild, headerGrand, footerRoot, footerChild} {
		if !containsKey(newKeys, projectID+"|"+id) {
			t.Errorf("被删行 %s 缺少按项失效", id)
		}
	}
	if containsKey(newKeys, projectID+"|"+keep) {
		t.Errorf("旁支行 %s 不该被按项失效", keep)
	}
}

func mustMenuKeys(c *navStaleCounter) []string {
	menus, _ := c.snapshot()
	return menus
}

func mustNavKeys(c *navStaleCounter) []string {
	_, items := c.snapshot()
	return items
}

func containsKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

// TestNavigationBulkDeleteParentAndChildSelectedOnce 覆盖父子同时选中：
// 合并重叠子树，不因第二次命中已删空集而误判失败。
func TestNavigationBulkDeleteParentAndChildSelectedOnce(t *testing.T) {
	db, _, navSvc, projectID, _ := newNavigationEnvPorts(t)
	parent := addNavItemKind(t, navSvc, projectID, "header", "父", "/dup-parent", nil)
	child := addNavItemKind(t, navSvc, projectID, "header", "子", "/dup-child", &parent)
	grand := addNavItemKind(t, navSvc, projectID, "header", "孙", "/dup-grand", &child)

	if err := navSvc.BulkDelete(context.Background(), &navigationdto.BulkDeleteReq{
		IDs: []string{parent, child},
	}); err != nil {
		t.Fatalf("父子同时选中应成功（合并重叠子树）: %v", err)
	}
	for _, id := range []string{parent, child, grand} {
		if navExists(t, db, id) {
			t.Fatalf("子树行 %s 应被删除", id)
		}
	}
}

// TestNavigationBulkDeleteEmptyOrMissingWritesNothing 覆盖整批零提交：
// 空 ID / 不存在项拒绝时，同批中合法行也不能被删除。
func TestNavigationBulkDeleteEmptyOrMissingWritesNothing(t *testing.T) {
	db, _, navSvc, projectID, _ := newNavigationEnvPorts(t)
	keep := addNavItemKind(t, navSvc, projectID, "header", "合法行", "/keep-me", nil)

	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"含空串", []string{keep, ""}},
		{"含不存在", []string{keep, "00000000-0000-0000-0000-0000000000aa"}},
		{"含非法 uuid", []string{keep, "not-a-uuid"}},
		// 单条空串：按 uuid 身份去重后就是 1 项，主消息给该条具体原因。
		{"单条空串", []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := navSvc.BulkDelete(context.Background(), &navigationdto.BulkDeleteReq{IDs: tc.ids})
			if err == nil {
				t.Fatal("混合非法项应整批拒绝")
			}
			var blocked facing.BlockedError
			if !errors.As(err, &blocked) || len(blocked.BlockedItems()) == 0 {
				t.Fatalf("拒绝应带可定位明细，实际 %T %v", err, err)
			}
			if !navExists(t, db, keep) {
				t.Fatal("整批拒绝后合法行不该被删除")
			}
		})
	}
}

// TestNavigationBulkDeleteSameUUIDAliasDeletesOnce 覆盖合法重复：
// 同一 uuid 的大小写别名在单条请求里仍是同一身份，成功删除一次而不是被拒绝。
func TestNavigationBulkDeleteSameUUIDAliasDeletesOnce(t *testing.T) {
	db, _, navSvc, projectID, _ := newNavigationEnvPorts(t)
	keep := addNavItemKind(t, navSvc, projectID, "header", "合法别名", "/alias", nil)

	alias := strings.ToUpper(keep)
	if alias == keep {
		t.Fatalf("夹具不成立：%q 无大小写差异，无法验证 uuid 身份去重", keep)
	}
	if !strings.EqualFold(alias, keep) {
		t.Fatalf("夹具不成立：%q 与 %q 不是同一 uuid", alias, keep)
	}
	probe := attachNavDeleteProbe(t, db, keep)

	if err := navSvc.BulkDelete(context.Background(), &navigationdto.BulkDeleteReq{
		IDs: []string{keep, alias},
	}); err != nil {
		t.Fatalf("合法重复别名应成功去重删除: %v", err)
	}
	if deletes, rows := probe.snap(); deletes != 1 || rows != 1 {
		t.Fatalf("同一身份的两写法只应下发一次删除并影响 1 行：DELETE 次数 %d、行数 %d", deletes, rows)
	}
	if navExists(t, db, keep) {
		t.Fatal("合法别名应删除该行")
	}
}

// TestNavigationBulkDeleteSecondKindFailureRollsBackFirst 覆盖：
// 页脚（字典序在前）已真实删除，页眉在标记菜单失效时故障，整批回滚——
// 用真实事务与故障端口见证，不靠猜 DELETE 行顺序。
func TestNavigationBulkDeleteSecondKindFailureRollsBackFirst(t *testing.T) {
	// 不编译页面：本用例只关心「菜单失效是否与删除同事务」。
	// 页面用真实生产建表 + 真实页面服务创建，依赖行按生产 schema 插入。
	db, _, navSvc, projectID, _ := newNavigationEnvPorts(t)
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))

	headerRoot := addNavItemKind(t, navSvc, projectID, "header", "页眉根", "/rb-h", nil)
	footerRoot := addNavItemKind(t, navSvc, projectID, "footer", "页脚根", "/rb-f", nil)

	pages := newAtomicityPageService(t, db, projects, navSvc)
	pageID := createNavPage(t, pages, projectID, "home", "/rb-page", customNavDocument)
	artifactID := insertAtomicityArtifact(t, db, pageID)
	if err := db.Exec(`INSERT INTO page_dependencies
		(page_id, artifact_id, dependency_kind, dependency_key, last_checked)
		VALUES (?, ?, 'menu', ?, NOW())`,
		pageID, artifactID, "menu:"+projectID+":footer").Error; err != nil {
		t.Fatalf("准备菜单依赖行失败: %v", err)
	}
	markPageActivated(t, db, pageID)
	if navPageStale(t, db, pageID) {
		t.Fatal("前置条件不成立：页面仍为待重建")
	}

	// 页脚（字典序在前）先完成删除与失效；页眉在标记菜单失效时故障。
	deleteProbe := attachNavDeleteProbe(t, db, footerRoot)
	attachFailQueue(t, navSvc, navWritePortsFor(db, projects), "header")

	err := navSvc.BulkDelete(ctx, &navigationdto.BulkDeleteReq{IDs: []string{headerRoot, footerRoot}})
	if err == nil {
		t.Fatal("第二个 kind 失效故障应让整批失败")
	}
	// 故障之前，页脚那条 DELETE 必须在真实事务里执行过，否则「回滚」无从谈起。
	deletes, deletedRows := deleteProbe.snap()
	if deletes == 0 || deletedRows != 1 {
		t.Fatalf("后 kind 故障前应先真实删除前 kind 的根：DELETE 次数 %d、影响行数 %d", deletes, deletedRows)
	}
	for _, id := range []string{headerRoot, footerRoot} {
		if !navExists(t, db, id) {
			t.Fatalf("整批回滚后行 %s 应仍在（第一个 kind 的删除也要回滚）", id)
		}
	}
	if navPageStale(t, db, pageID) {
		t.Fatal("整批回滚后页面不该停在待重建（菜单失效标记必须同事务回滚）")
	}
}

// attachNavDeleteProbe 记录真实下发到某个 id 的 DELETE 次数与行数，
// 作为「前一个 kind 已执行删除、随后整批回滚」的执行见证。
func attachNavDeleteProbe(t *testing.T, db *gorm.DB, id string) *navDeleteProbe {
	t.Helper()
	probe := &navDeleteProbe{}
	const cbName = "test:nav_delete_probe"
	cb := func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "navigations" {
			return
		}
		if !strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "DELETE") {
			return
		}
		if !statementBindsValue(tx.Statement.Vars, id) {
			return
		}
		probe.mu.Lock()
		probe.deletes++
		probe.rows = tx.RowsAffected
		probe.mu.Unlock()
	}
	if err := db.Callback().Delete().After("gorm:delete").Register(cbName, cb); err != nil {
		t.Fatalf("注册删除见证回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(cbName) })
	return probe
}

type navDeleteProbe struct {
	mu      sync.Mutex
	deletes int
	rows    int64
}

func (p *navDeleteProbe) snap() (int, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.deletes, p.rows
}

type sourceResolverStub struct {
	groups map[string][]navigationcontract.SourceCandidate
}

func (s sourceResolverStub) Candidates(_ context.Context, _ string) ([]navigationcontract.SourceGroup, error) {
	out := make([]navigationcontract.SourceGroup, 0, len(s.groups))
	for groupType, items := range s.groups {
		out = append(out, navigationcontract.SourceGroup{Type: groupType, Items: items})
	}
	return out, nil
}

func (s sourceResolverStub) ResolveSource(_ context.Context, _, _, _ string) (string, string, error) {
	return "", "", nil
}

func navAddSourceIDs(t *testing.T, svc navigationcontract.NavigationService, projectID, kind string) []string {
	t.Helper()
	items, err := svc.List(context.Background(), &navigationdto.ListReq{ProjectID: projectID, Kind: kind})
	if err != nil {
		t.Fatalf("列导航项失败: %v", err)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

const (
	// 含 a-f 字母，大小写两种写法才是「原串不同、uuid 身份相同」的真实别名。
	sourcePageA = "aaaaaaaa-1111-4a11-8a11-111111111111"
	sourcePageB = "bbbbbbbb-2222-4b22-8b22-222222222222"
	sourceDraft = "cccccccc-3333-4c33-8c33-333333333333"
)

func newAddSourceEnv(t *testing.T) (*gorm.DB, navigationcontract.NavigationService, navWritePortFixture, string) {
	t.Helper()
	db, _, navSvc, projectID, ports := newNavigationEnvPorts(t)
	navSvc.SetSourceResolver(sourceResolverStub{groups: map[string][]navigationcontract.SourceCandidate{
		"page": {
			{ID: sourcePageA, Label: "/a", Title: "A 页", URL: "/a"},
			{ID: sourcePageB, Label: "/b", Title: "B 页", URL: "/b"},
			{ID: sourceDraft, Label: "/draft", Title: "草稿页", URL: ""},
		},
	}})
	return db, navSvc, ports, projectID
}

// TestNavigationAddSourcesDedupesUUIDIdentity 覆盖：同一 uuid 的大小写别名只写一行，
// 合法重复可去重，added 是实际写入行数。
func TestNavigationAddSourcesDedupesUUIDIdentity(t *testing.T) {
	db, navSvc, _, projectID := newAddSourceEnv(t)
	before := len(navAddSourceIDs(t, navSvc, projectID, "header"))

	alias := strings.ToUpper(sourcePageA)
	if alias == sourcePageA {
		t.Fatalf("夹具不成立：%q 的大小写写法完全相同，无法验证 uuid 身份去重", sourcePageA)
	}
	if !strings.EqualFold(alias, sourcePageA) {
		t.Fatalf("夹具不成立：%q 与 %q 不是同一 uuid 的大小写别名", alias, sourcePageA)
	}
	added, err := navSvc.AddSources(context.Background(), &navigationdto.AddSourceReq{
		ProjectID: projectID, Kind: "header", SourceType: "page",
		// 同一实体的大小写别名：按数据库 uuid 身份去重，而不是字符串别名。
		IDs: []string{sourcePageA, alias},
	})
	if err != nil {
		t.Fatalf("合法批量加入失败: %v", err)
	}
	if added != 1 {
		t.Fatalf("大小写别名应去重成 1 行，实际 added=%d", added)
	}
	after := len(navAddSourceIDs(t, navSvc, projectID, "header"))
	if after != before+1 {
		t.Fatalf("应只写入 1 行，实际 %d → %d", before, after)
	}
	var bound string
	if err := db.Raw(`SELECT id FROM navigations WHERE project_id = ? AND path = ?`, projectID, "/a").
		Scan(&bound).Error; err != nil {
		t.Fatalf("读回来源行失败: %v", err)
	}
	if bound == "" {
		t.Fatal("来源行应真的落库")
	}
}

// TestNavigationAddSourcesRejectsWholeBatch 覆盖混合拒绝：缺失、无公开路径、
// 已有路径冲突、批内重复路径都整批失败且零写入。
func TestNavigationAddSourcesRejectsWholeBatch(t *testing.T) {
	_, navSvc, _, projectID := newAddSourceEnv(t)
	ctx := context.Background()

	if _, err := navSvc.AddSources(ctx, &navigationdto.AddSourceReq{
		ProjectID: projectID, Kind: "header", SourceType: "page", IDs: []string{sourcePageA},
	}); err != nil {
		t.Fatalf("首次加入失败: %v", err)
	}
	before := len(navAddSourceIDs(t, navSvc, projectID, "header"))

	for _, tc := range []struct {
		name   string
		ids    []string
		reason string
		status int
	}{
		{"无公开路径", []string{sourcePageB, sourceDraft}, navigationenums.NoPublicPath, 400},
		{"缺失候选", []string{sourcePageB, "00000000-0000-0000-0000-0000000000aa"}, navigationenums.ErrNotFound, 404},
		{"已有路径", []string{sourcePageB, sourcePageA}, navigationenums.ErrPathTaken, 409},
		{"空标识", []string{sourcePageB, ""}, navigationenums.ErrInvalidParam, 400},
		{"非法 uuid", []string{sourcePageB, "nope"}, navigationenums.ErrInvalidParam, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := navSvc.AddSources(ctx, &navigationdto.AddSourceReq{
				ProjectID: projectID, Kind: "header", SourceType: "page", IDs: tc.ids,
			})
			if err == nil {
				t.Fatalf("应整批拒绝（%s）", tc.reason)
			}
			var blocked facing.BlockedError
			if !errors.As(err, &blocked) || len(blocked.BlockedItems()) == 0 {
				t.Fatalf("拒绝应带可定位明细，实际 %T %v", err, err)
			}
			var statused facing.Statused
			if !errors.As(err, &statused) || statused.HTTPStatus() != tc.status {
				t.Fatalf("多项拒绝状态应为 %d，实际 %T %v", tc.status, err, err)
			}
			if !blockedItemHasReason(blocked.BlockedItems(), tc.reason) {
				t.Fatalf("明细应含原因 %q，实际 %+v", tc.reason, blocked.BlockedItems())
			}
			if got := len(navAddSourceIDs(t, navSvc, projectID, "header")); got != before {
				t.Fatalf("整批拒绝后条数应不变，实际 %d → %d", before, got)
			}
		})
	}
}

func blockedItemHasReason(items []facing.BlockedItem, reason string) bool {
	for _, item := range items {
		if item.ReasonKey == reason {
			return true
		}
	}
	return false
}

// TestNavigationAddSourcesStaleFailureRollsBackWrites 覆盖：全部行插入后，
// 菜单失效/入队故障让整批回滚，不留半批数据。
func TestNavigationAddSourcesStaleFailureRollsBackWrites(t *testing.T) {
	db, navSvc, ports, projectID := newAddSourceEnv(t)
	attachFailQueue(t, navSvc, ports, "footer")

	before := len(navAddSourceIDs(t, navSvc, projectID, "footer"))
	added, err := navSvc.AddSources(context.Background(), &navigationdto.AddSourceReq{
		ProjectID: projectID, Kind: "footer", SourceType: "page",
		IDs: []string{sourcePageA, sourcePageB},
	})
	if err == nil {
		t.Fatal("菜单失效故障应让整批失败")
	}
	if added != 0 {
		t.Fatalf("整批失败时 added 必须为 0，实际 %d", added)
	}
	if got := len(navAddSourceIDs(t, navSvc, projectID, "footer")); got != before {
		t.Fatalf("回滚后不该留下半批数据，实际 %d → %d", before, got)
	}
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM navigations WHERE project_id = ?`, projectID).Scan(&count).Error; err != nil {
		t.Fatalf("统计导航行失败: %v", err)
	}
	if count != int64(before) {
		t.Fatalf("工程内导航行数应回到写入前，实际 %d（写入前 %d）", count, before)
	}
}

// TestNavigationAddSourcesConcurrentSamePath 覆盖并发同路径：工程内容写锁把两批
// 串行化，最多一批成功，路径只落一行。两端都用有界 ctx，失败时明确结束等待。
func TestNavigationAddSourcesConcurrentSamePath(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	projectID := createProjectForAtomicity(t, db)

	newSvc := func() navigationcontract.NavigationService {
		impl := navigationservice.NewService(navigationmodel.NewModel(db), projects)
		attachNavWritePorts(t, impl, db, projects)
		impl.SetSourceResolver(sourceResolverStub{groups: map[string][]navigationcontract.SourceCandidate{
			"page": {{ID: sourcePageA, Label: "/a", Title: "A 页", URL: "/a"}},
		}})
		return impl
	}
	first := newSvc()
	second := newSvc()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type result struct {
		added int
		err   error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, svc := range []navigationcontract.NavigationService{first, second} {
		go func(svc navigationcontract.NavigationService) {
			<-start
			added, err := svc.AddSources(ctx, &navigationdto.AddSourceReq{
				ProjectID: projectID, Kind: "header", SourceType: "page", IDs: []string{sourcePageA},
			})
			results <- result{added: added, err: err}
		}(svc)
	}
	close(start)

	success, failure := 0, 0
	for i := 0; i < 2; i++ {
		select {
		case res := <-results:
			if res.err == nil {
				success++
				if res.added != 1 {
					t.Fatalf("成功的一批应恰好写入 1 行，实际 added=%d", res.added)
				}
			} else {
				failure++
			}
		case <-ctx.Done():
			t.Fatalf("并发同路径在 30s 内未结束，ctx 错误: %v", ctx.Err())
		}
	}
	if success != 1 || failure != 1 {
		t.Fatalf("并发同路径应恰好一批成功，实际 success=%d failure=%d", success, failure)
	}
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM navigations WHERE project_id = ? AND path = ?`, projectID, "/a").
		Scan(&count).Error; err != nil {
		t.Fatalf("统计同路径行失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("同路径只应落一行，实际 %d", count)
	}
}

// TestNavigationAddSourcesSecondInsertFailureRollsBackFirst 覆盖：
// 第一项已真实插入后，第二项插入失败，整批回滚、added=0，第一项也不留。
func TestNavigationAddSourcesSecondInsertFailureRollsBackFirst(t *testing.T) {
	db, navSvc, _, projectID := newAddSourceEnv(t)
	before := len(navAddSourceIDs(t, navSvc, projectID, "header"))
	probe := attachNavInsertFailure(t, db, "/b")

	added, err := navSvc.AddSources(context.Background(), &navigationdto.AddSourceReq{
		ProjectID: projectID, Kind: "header", SourceType: "page",
		IDs: []string{sourcePageA, sourcePageB},
	})
	if err == nil {
		t.Fatal("第二项插入故障应让整批失败")
	}
	if added != 0 {
		t.Fatalf("整批失败时 added 必须为 0，实际 %d", added)
	}
	inserts, rows, lastPath, injected, injectedPath := probe.snap()
	if inserts != 1 || rows != 1 || lastPath != "/a" {
		t.Fatalf("故障前第一项必须真实插入成功：inserts=%d rows=%d last=%q", inserts, rows, lastPath)
	}
	if injected != 1 || injectedPath != "/b" {
		t.Fatalf("故障应在第二项 INSERT 前恰好注入一次：injected=%d path=%q", injected, injectedPath)
	}
	if !errors.Is(err, errNavInsertInjected) {
		t.Fatalf("错误应来自注入的第二项插入故障，实际 %v", err)
	}
	if got := len(navAddSourceIDs(t, navSvc, projectID, "header")); got != before {
		t.Fatalf("回滚后不留半批数据，实际 %d → %d", before, got)
	}
	for _, path := range []string{"/a", "/b"} {
		var n int64
		if err := db.Raw(`SELECT COUNT(*) FROM navigations WHERE project_id = ? AND path = ?`, projectID, path).
			Scan(&n).Error; err != nil {
			t.Fatalf("统计路径 %s 失败: %v", path, err)
		}
		if n != 0 {
			t.Fatalf("回滚后路径 %s 不该有行，实际 %d", path, n)
		}
	}
}

// attachNavInsertFailure 在 failPath 这条 INSERT 真正下发数据库之前注入故障，
// 并独立记录前一项成功 INSERT 的 RowsAffected，作为「前项已执行」的真实见证。
func attachNavInsertFailure(t *testing.T, db *gorm.DB, failPath string) *navInsertProbe {
	t.Helper()
	probe := &navInsertProbe{}
	const beforeName = "test:nav_insert_failure_before"
	const afterName = "test:nav_insert_failure_after"

	pathOf := func(tx *gorm.DB) string {
		if tx.Statement == nil || tx.Statement.Table != "navigations" {
			return ""
		}
		entity, ok := tx.Statement.Dest.(*navigationmodel.NavigationEntity)
		if !ok || entity == nil {
			return ""
		}
		return strings.TrimSpace(entity.Path)
	}

	// Before gorm:create：目标行尚未触库；在这里置错，数据库 INSERT 本身不会发生。
	before := func(tx *gorm.DB) {
		if pathOf(tx) != failPath {
			return
		}
		probe.mu.Lock()
		probe.injected++
		probe.injectedPath = failPath
		probe.mu.Unlock()
		tx.AddError(errNavInsertInjected)
	}
	// After gorm:create：只在真正执行过 INSERT 后回调，记录成功行数。
	after := func(tx *gorm.DB) {
		path := pathOf(tx)
		if path == "" || path == failPath {
			return
		}
		probe.mu.Lock()
		probe.inserts++
		probe.rows += tx.RowsAffected
		probe.lastPath = path
		probe.mu.Unlock()
	}
	if err := db.Callback().Create().Before("gorm:create").Register(beforeName, before); err != nil {
		t.Fatalf("注册插入故障回调失败: %v", err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(afterName, after); err != nil {
		t.Fatalf("注册插入见证回调失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(beforeName)
		_ = db.Callback().Create().Remove(afterName)
	})
	return probe
}

var errNavInsertInjected = errors.New("injected: navigation insert failed")

type navInsertProbe struct {
	mu           sync.Mutex
	inserts      int
	rows         int64
	lastPath     string
	injected     int
	injectedPath string
}

// snap 返回：成功 INSERT 次数、成功行数合计、最后成功的路径、注入次数、注入路径。
func (p *navInsertProbe) snap() (int, int64, string, int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inserts, p.rows, p.lastPath, p.injected, p.injectedPath
}

// TestNavigationDeleteRevalidatesKindUnderLock 覆盖：事务外定位后并发改 kind，
// 进写锁后必须按锁内最新 kind 复核，而不是沿用事务外的旧 kind。
func TestNavigationDeleteRevalidatesKindUnderLock(t *testing.T) {
	db, _, navSvc, projectID, ports := newNavigationEnvPorts(t)
	counter := &navStaleCounter{}
	navSwapAttachCounter(t, navSvc, ports, counter)

	itemID := addNavItemKind(t, navSvc, projectID, "header", "并发改位", "/concurrent-kind", nil)
	baseMenus, baseItems := counter.snapshot()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	holder, err := newAdvisoryLockHolder(ctx, t, db, projectID, "footer", itemID)
	if err != nil {
		t.Fatalf("准备持锁连接失败: %v", err)
	}
	defer holder.close()

	done := make(chan error, 1)
	go func() {
		done <- navSvc.Delete(ctx, &navigationdto.DeleteReq{ID: itemID})
	}()
	if err := waitForWaitingOnLock(ctx, t, db, holder.key); err != nil {
		t.Fatalf("删除未进入等锁状态: %v", err)
	}
	if err := holder.commit(); err != nil {
		t.Fatalf("提交 kind 变更失败: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("锁内复核后删除应成功: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("删除操作在 30s 内未结束: %v", ctx.Err())
	}

	if navExists(t, db, itemID) {
		t.Fatal("删除后行应不存在")
	}
	menus, items := counter.snapshot()
	newMenus := append([]string(nil), menus[len(baseMenus):]...)
	newItems := append([]string(nil), items[len(baseItems):]...)
	if !containsKey(newMenus, projectID+"|footer") {
		t.Fatalf("必须按锁内最新 kind（footer）标记菜单失效，实际新增 %v", newMenus)
	}
	if containsKey(newMenus, projectID+"|header") {
		t.Fatalf("不得按事务外旧 kind（header）标记失效，实际新增 %v", newMenus)
	}
	if !containsKey(newItems, projectID+"|"+itemID) {
		t.Fatalf("被删行缺少按项失效，实际新增 %v", newItems)
	}
}

// TestNavigationDeleteRejectsRowDeletedUnderLock 覆盖：事务外定位成功后，
// 另一事务先删该行并提交；进写锁复核时必须整批拒绝而不是静默成功。
func TestNavigationDeleteRejectsRowDeletedUnderLock(t *testing.T) {
	db, _, navSvc, projectID, _ := newNavigationEnvPorts(t)
	itemID := addNavItemKind(t, navSvc, projectID, "header", "并发删除", "/concurrent-delete", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	holder, err := newAdvisoryLockHolder(ctx, t, db, projectID, "header", itemID)
	if err != nil {
		t.Fatalf("准备持锁连接失败: %v", err)
	}
	defer holder.close()
	if _, err := holder.tx.ExecContext(ctx, "DELETE FROM navigations WHERE id = $1", itemID); err != nil {
		t.Fatalf("持锁连接删除行失败: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- navSvc.Delete(ctx, &navigationdto.DeleteReq{ID: itemID})
	}()
	if err := waitForWaitingOnLock(ctx, t, db, holder.key); err != nil {
		t.Fatalf("删除未进入等锁状态: %v", err)
	}
	if err := holder.commit(); err != nil {
		t.Fatalf("提交并发删除失败: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("锁内复核发现行已不存在时必须拒绝，不能静默成功")
		}
		var blocked facing.BlockedError
		if !errors.As(err, &blocked) || len(blocked.BlockedItems()) == 0 {
			t.Fatalf("拒绝应带可定位明细，实际 %T %v", err, err)
		}
	case <-ctx.Done():
		t.Fatalf("删除操作在 30s 内未结束: %v", ctx.Err())
	}
}

// insertAtomicityArtifact 按生产 schema 的必填列插入一条产物行，供依赖外键引用。
func insertAtomicityArtifact(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	artifactID := uuid.NewString()
	if err := db.Exec(`
		INSERT INTO page_artifacts (id, page_id, version, source_document, page_document_schema_version,
			source_hash, build_input_manifest, build_input_hash, artifact_provider, artifact_key,
			artifact_hash, compiler_version, registry_version, manifest, created_by, create_time)
		VALUES (?, ?, 1, '{}'::jsonb, 1, ?, '{}'::jsonb, ?, 'local', ?, ?, 'c1', 'r1', '{}'::jsonb, ?, NOW())`,
		artifactID, pageID, "h-src-"+artifactID[:8], "h-build-"+artifactID[:8],
		"key-"+artifactID, "h-art-"+artifactID[:8], uuid.NewString()).Error; err != nil {
		t.Fatalf("准备产物行失败: %v", err)
	}
	return artifactID
}

// newAtomicityPageService 用真实生产依赖构造页面服务；只创建页面，不做编译。
func newAtomicityPageService(t *testing.T, db *gorm.DB,
	projects projectcontract.ProjectService, navSvc navigationcontract.NavigationService) pagecontract.PageService {
	t.Helper()
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks,
		nil, nil, navSvc, nil)
}

func createProjectForAtomicity(t *testing.T, db *gorm.DB) string {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	p, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "导航原子性工程"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}
	return p.ID
}
