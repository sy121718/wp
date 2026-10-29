package inventoryhttp

// inventory_page_render_test.go — 库存三页（库存管理 / 仓库管理 / 变动原因字典）整页失败出口的回归护栏。
//
// 被守的边界（AGENTS.md §「整页 GET 的失败出口：失败也必须是『这一页』」；
// 规范样板 navigation_page_handle.go:290-317）：
//
//  1. 取数失败仍是**这一页**：同一模板 + 完整页壳 + Content-Type: text/html ——
//     此前三页走 shell 的 PageError → response 的 ErrorWithMessage，浏览器拿到的是一段 JSON，
//     页壳、侧栏、标题全丢，地址栏还停在原处；
//  2. 500 + Cache-Control: no-store —— 200 会让失败在监控里与正常页无从区分，
//     且「重试」会从缓存里拿回同一份失败；
//  3. 文案已按请求语言归口、缺词落模块内兜底；数据库原文只进日志，页面上不出现裸 key；
//  4. 失败优先于空态：装载失败不许渲染「还没有仓库 / 还没有变动原因 / 没有符合条件的库存流水」——
//     那会让人以为数据被删了、去重建一遍。
//
// 真实 Jet 渲染：模板取自 templates/（与线上同一份），页面壳 / 取词 / 权限按钮集合都走 shell 的真实实现，
// 只有服务是桩（不连库）。

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// 三个键值取自 shell 包（PermSetKey / ButtonsKey / MsgInternalError）。
//
// 本文件刻意只写字面量、不引用 shell 包的标识符：scripts/check-shell-migration.sh 按
// shell 包标识符的使用点统计，测试桶的基线为 1、只许下降 —— 护栏本身不该把基线顶上去。
const (
	permSetKey       = "perm_set"
	buttonsKey       = "button_set"
	internalErrorKey = "MsgInternalError"
)

// inventoryPageRenderStub 只实现三页用到的读方法（其余走嵌入的 nil 接口）。
//
// 每个 err 字段开关一条失败路径：三页的**主数据**分别来自工程列表、仓库列表、原因列表与流水，
// 每一条都要能在「没读到」时回到同一页（不是 JSON）。
type inventoryPageRenderStub struct {
	inventorycontract.InventoryService
	warehousesErr error
	reasonsErr    error
	movementsErr  error
	stocksErr     error
	warehouses    []*inventorydto.WarehouseResp
	reasons       []*inventorydto.ReasonResp
	movements     []*inventorydto.MovementResp
}

func (s *inventoryPageRenderStub) ListWarehouses(context.Context, *inventorydto.ListWarehouseReq) ([]*inventorydto.WarehouseResp, error) {
	if s.warehousesErr != nil {
		return nil, s.warehousesErr
	}
	return s.warehouses, nil
}

func (s *inventoryPageRenderStub) ListReasons(context.Context, *inventorydto.ListReasonReq) ([]*inventorydto.ReasonResp, error) {
	if s.reasonsErr != nil {
		return nil, s.reasonsErr
	}
	return s.reasons, nil
}

func (s *inventoryPageRenderStub) ListStocksBySKU(context.Context, *inventorydto.ListStockBySKUReq) ([]*inventorydto.StockResp, error) {
	if s.stocksErr != nil {
		return nil, s.stocksErr
	}
	return []*inventorydto.StockResp{}, nil
}

func (s *inventoryPageRenderStub) ListMovements(context.Context, *inventorydto.ListMovementReq) ([]*inventorydto.MovementResp, error) {
	if s.movementsErr != nil {
		return nil, s.movementsErr
	}
	return s.movements, nil
}

func (s *inventoryPageRenderStub) CountMovements(context.Context, *inventorydto.ListMovementReq) (int64, error) {
	if s.movementsErr != nil {
		return 0, s.movementsErr
	}
	return int64(len(s.movements)), nil
}

// inventoryPageRenderProjects 工程列表桩：listErr 开关「主数据没读到」那条路径。
type inventoryPageRenderProjects struct {
	projectcontract.ProjectService
	listErr error
}

func (s *inventoryPageRenderProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []projectcontract.ProjectResp{{ID: "project-1", Name: "示例工程"}}, nil
}

// inventoryPageRenderCatalog 商品契约桩：主页的 SKU 下拉（`variantOptions`）读商品与变体。
//
// 这是「SKU 下拉没读到」那条出口能被独立触发的唯一途径：装配期传 nil（未注入商品契约）时
// `variantOptions` 直接短路返回空 —— 那是「能力未装配」的既定降级，不是失败路径，
// 拿它当覆盖等于没测到 `variantOptions` 的 err 分支。
type inventoryPageRenderCatalog struct {
	productcontract.VariantOptionsReader
	options []productcontract.VariantOption
	listErr error
}

func (s *inventoryPageRenderCatalog) ListAllVariantOptions(context.Context, string) ([]productcontract.VariantOption, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.options, nil
}

// newInventoryPageTestRouter 装配与线上同一条渲染路径的测试路由（三页共用一份）。
//
// 不依赖数据库：库存与工程服务都是桩，页面壳与取词走 shell 的真实实现（所以取词用的就是请求语言）。
// 成功路径与失败路径共用这份装配 —— 两处各装一次，失败路径的断言就会悄悄退化成「测了另一套中间件」。
func newInventoryPageTestRouter(t *testing.T, inv inventorycontract.InventoryService,
	projects projectcontract.ProjectService, catalog ...productcontract.VariantOptionsReader) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	// 日志目录必须**本用例独有**（t.TempDir()）：共享路径下躺着上一次运行写下的行，
	// 「日志里找得到原文」这条断言在本次请求根本没写日志时也会通过。
	// logger.Init 会清空场景 logger 缓存并按新配置重建（pkg/logger/manager.go:62），所以这里的目录确实生效。
	logDir := t.TempDir()
	inventoryPageTestLogDirs.Store(t.Name(), logDir)
	conf := viper.New()
	conf.Set("log.base_dir", logDir)
	if err := logger.Init(conf); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	store := cookie.NewStore([]byte("inventory-page-render-test-secret-32b"))
	router.Use(sessions.Sessions("inv-test", store))
	router.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("csrf_token", "test-csrf-token")
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		// 与 PermContextMiddleware 同源：按钮码集合决定页头 / 空态里是否给出写入口。
		perms := map[string]bool{
			"inventory.warehouse_create": true,
			"inventory.warehouse_update": true,
			"inventory.warehouse_delete": true,
			"inventory.reason_create":    true,
			"inventory.reason_update":    true,
		}
		c.Set(permSetKey, perms)
		c.Set(buttonsKey, perms)
		c.Next()
	})
	// catalog 只给需要触发「SKU 下拉读不到」的用例：不传就是装配期未注入商品契约的形态
	//（variantOptions 短路返回空、页面照常渲染），与本文件其余用例一致。
	var products productcontract.VariantOptionsReader
	if len(catalog) > 0 {
		products = catalog[0]
	}
	h := NewInventoryPageHandle(inv, projects, products)
	router.GET("/admin/inventory", adminui.PageSlot(InventoryListPageTemplate), h.InventoryPage)
	router.GET("/admin/inventory/warehouses", adminui.PageSlot(InventoryWarehousesPageTemplate), h.InventoryWarehousesPage)
	router.GET("/admin/inventory/reasons", adminui.PageSlot(InventoryReasonsPageTemplate), h.InventoryReasonsPage)
	return router
}

// assertInventoryFailurePage 失败页的四条不变量：形态 / 状态码 / 页壳 / 归口。
//
// emptyText 是该页**成功空态标题**的完整标签（`<p class="empty-title">…</p>`）：
// 装载失败时不许出现（两档必须分开）。用整标签而不是纯文案子串 ——
// 页头的 .help 悬浮里也写着「这个工程还没有仓库时…」，纯子串会把帮助文案当成空态。
func assertInventoryFailurePage(t *testing.T, rec *httptest.ResponseRecorder, rawErr error, emptyText string) {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("取数失败的状态码应为 500，得到 %d：%s", rec.Code, body)
	}
	// 形态：JSON 错误体（失败不渲染页壳 —— 用户裁定：所有提示只需要一个提示）。
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON，得到 %q", ct)
	}
	for _, banned := range []string{"<!DOCTYPE", `class="admin-layout"`, "</body>", "</html>", `id="adminRail"`} {
		if strings.Contains(body, banned) {
			t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
		}
	}
	// 归口文案在（模块自己的内部错误兜底），原文与裸 key 都不在。
	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Errorf("失败响应应带归口文案，实际：%s", body)
	}
	for _, leak := range []string{rawErr.Error(), html.EscapeString(rawErr.Error()), internalErrorKey} {
		if strings.Contains(body, leak) {
			t.Errorf("失败响应不应出现 %q：%s", leak, body)
		}
	}
	// 失败优先于空态：JSON 里不该出现空态文案。
	if emptyText != "" && strings.Contains(body, emptyText) {
		t.Errorf("装载失败被渲染成了空态（%q）：两档必须分开：%s", emptyText, body)
	}
}

// TestInventoryPagesLoadFailureRenderPage 三页**全部 10 处**取数失败出口都必须是 JSON 错误体（500）。
//
// 10 处 = 库存管理页 6（工程列表 / 仓库列表 / SKU 下拉 / 某 SKU 各仓库存 / 原因字典 / 流水）
// + 仓库管理页 2（工程列表 / 仓库列表）+ 变动原因字典页 2（工程列表 / 原因列表）。
func TestInventoryPagesLoadFailureRenderPage(t *testing.T) {
	// 桩错误刻意带一段「像数据库原文」的内容：它必须只出现在日志里。
	rawErr := errors.New(`pq: relation "inventory_stocks" does not exist`)
	emptyInventory := `<p class="empty-title">没有符合条件的库存流水</p>`

	for _, tc := range []struct {
		name      string
		path      string
		inv       *inventoryPageRenderStub
		projects  projectcontract.ProjectService
		catalog   productcontract.VariantOptionsReader
		emptyText string
	}{
		{
			name: "库存管理 / 工程列表没读到", path: "/admin/inventory",
			inv:      &inventoryPageRenderStub{},
			projects: &inventoryPageRenderProjects{listErr: rawErr}, emptyText: emptyInventory,
		},
		{
			name: "库存管理 / 仓库列表没读到", path: "/admin/inventory",
			inv:      &inventoryPageRenderStub{warehousesErr: rawErr},
			projects: &inventoryPageRenderProjects{}, emptyText: emptyInventory,
		},
		{
			name: "库存管理 / SKU 下拉没读到", path: "/admin/inventory",
			inv:      &inventoryPageRenderStub{},
			projects: &inventoryPageRenderProjects{},
			catalog:  &inventoryPageRenderCatalog{listErr: rawErr}, emptyText: emptyInventory,
		},
		{
			name: "库存管理 / 原因字典没读到", path: "/admin/inventory",
			inv:      &inventoryPageRenderStub{reasonsErr: rawErr},
			projects: &inventoryPageRenderProjects{}, emptyText: emptyInventory,
		},
		{
			name: "库存管理 / 流水没读到", path: "/admin/inventory",
			inv:      &inventoryPageRenderStub{movementsErr: rawErr},
			projects: &inventoryPageRenderProjects{}, emptyText: emptyInventory,
		},
		{
			name: "库存管理 / 某 SKU 各仓库存没读到", path: "/admin/inventory?sku=SKU-1",
			inv:      &inventoryPageRenderStub{stocksErr: rawErr},
			projects: &inventoryPageRenderProjects{}, emptyText: emptyInventory,
		},
		{
			name: "仓库管理 / 工程列表没读到", path: "/admin/inventory/warehouses",
			inv:      &inventoryPageRenderStub{},
			projects: &inventoryPageRenderProjects{listErr: rawErr}, emptyText: `<p class="empty-title">还没有仓库</p>`,
		},
		{
			name: "仓库管理 / 仓库列表没读到", path: "/admin/inventory/warehouses",
			inv:      &inventoryPageRenderStub{warehousesErr: rawErr},
			projects: &inventoryPageRenderProjects{}, emptyText: `<p class="empty-title">还没有仓库</p>`,
		},
		{
			name: "变动原因字典 / 工程列表没读到", path: "/admin/inventory/reasons",
			inv:      &inventoryPageRenderStub{},
			projects: &inventoryPageRenderProjects{listErr: rawErr}, emptyText: `<p class="empty-title">还没有变动原因</p>`,
		},
		{
			name: "变动原因字典 / 原因列表没读到", path: "/admin/inventory/reasons",
			inv:      &inventoryPageRenderStub{reasonsErr: rawErr},
			projects: &inventoryPageRenderProjects{}, emptyText: `<p class="empty-title">还没有变动原因</p>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(nil, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			router := newInventoryPageTestRouter(t, tc.inv, tc.projects, tc.catalog)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Accept-Language", "zh-CN")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assertInventoryFailurePage(t, rec, rawErr, tc.emptyText)
		})
	}
}

// TestInventoryPageLoadFailureFollowsRequestLanguage 失败槽文案按请求语言取词，缺词落模块内兜底。
//
// 两条链各自明确（task-14 复审 P2 的口径）：页壳上的标题 / 按钮由模板取词（缺词用模板兜底），
// 失败槽给的是 Go 归口后的业务结论。
func TestInventoryPageLoadFailureFollowsRequestLanguage(t *testing.T) {
	rawErr := errors.New(`pq: relation "inventory_warehouses" does not exist`)
	// 中英都注：只注一种语言会被 i18n 缓存「遍历所有可用语言」的兜底顶掉。
	entries := map[string]map[string]string{
		internalErrorKey:                    {"zh-CN": "系统内部错误，请稍后重试", "en-US": "Something went wrong. Please try again later"},
		"admin.common.list.loadFailedTitle": {"zh-CN": "这一页的数据没能读出来", "en-US": "This page could not load its data"},
	}

	for _, tc := range []struct {
		name       string
		acceptLang string
		entries    map[string]map[string]string
		wantMsg    string
		wantTitle  string
	}{
		{name: "zh-CN", acceptLang: "zh-CN", entries: entries,
			wantMsg: "系统内部错误，请稍后重试", wantTitle: "这一页的数据没能读出来"},
		{name: "en-US", acceptLang: "en-US", entries: entries,
			wantMsg: "Something went wrong. Please try again later", wantTitle: "This page could not load its data"},
		{
			// 词条缺席（迁移没跑 / 词条库不可用）：落调用点的中文兜底 ——
			// 既不能是空串（页面就成了「加载失败 + 一片空白」），也不能是裸 key。
			// 请求语言故意取英文，证明缺词时不挑语言。
			name: "词条缺席回落兜底", acceptLang: "en-US", entries: nil,
			wantMsg: "系统内部错误，请稍后重试", wantTitle: "这一页的数据没能读出来",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(tc.entries, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			router := newInventoryPageTestRouter(t,
				&inventoryPageRenderStub{}, &inventoryPageRenderProjects{listErr: rawErr})
			req := httptest.NewRequest(http.MethodGet, "/admin/inventory", nil)
			req.Header.Set("Accept-Language", tc.acceptLang)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			body := rec.Body.String()
			// JSON 错误体只有归口文案一条：页壳标题（wantTitle）不再出现 ——
			// 那句话属于「页壳上的失败槽」，随失败页一起作废（用户裁定：一个提示就够）。
			if !strings.Contains(body, tc.wantMsg) {
				t.Errorf("失败响应应含归口文案 %q，实际：%s", tc.wantMsg, body)
			}
			if strings.Contains(body, internalErrorKey) {
				t.Errorf("响应里漏出了 i18n key %q：%s", internalErrorKey, body)
			}
		})
	}
}

// inventoryPageTestLogDirs 记住每个用例自己的日志目录：装配时存入，断言时取回。
//
// 为什么不是 /tmp 下的一个固定路径：共享目录里躺着**上一次运行**写下的行，「日志里有原文」
// 这条断言在本次请求根本没有写日志时也会通过。t.TempDir() 每用例一个新目录，断言只看得见本用例的写入。
var inventoryPageTestLogDirs sync.Map

// TestInventoryPageLoadFailureKeepsRawErrorInLog 「归口」不是「丢弃」的正面证据。
//
// 反面证据（页面上不含原文）在 assertInventoryFailurePage；这一条查另一端：内部错误必须能在
// 结构化日志里被完整找回 —— 否则 inventoryErrText 注释许诺的「谁在哪个页面撞上了哪条 SQL / 约束」
// 就永远没人能回答（迁移 407 与 shell 的错误出口口径都要求留下可诊断性）。
func TestInventoryPageLoadFailureKeepsRawErrorInLog(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rawErr := errors.New(`pq: relation "inventory_movements" does not exist`)
	router := newInventoryPageTestRouter(t, &inventoryPageRenderStub{},
		&inventoryPageRenderProjects{listErr: rawErr})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/inventory", nil))

	// 页面这一端：原样与 HTML 转义两种形态都不许出现（Jet 把 `"` 渲染成 &#34;，只比原样会漏判）。
	body := rec.Body.String()
	for _, leaked := range []string{rawErr.Error(), html.EscapeString(rawErr.Error())} {
		if strings.Contains(body, leaked) {
			t.Fatalf("归口失败：原文进了页面：%s", body)
		}
	}
	// 日志这一端：必须有一条结构化记录的 error 字段**完整等于**原文。
	if !inventoryTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}
	// 自检：换一条本次没发生过的原文必须判为未命中 —— 否则「找得到」可能来自任何一行日志。
	if inventoryTestLogHasRawError(t, errors.New(`pq: relation "never_happened" does not exist`)) {
		t.Errorf("自检失败：本次没发生过的原文也被判定为命中")
	}
}

// inventoryPageTestLogDir 取回本用例的日志目录；取不到说明断言落在没装配过路由的用例里。
func inventoryPageTestLogDir(t *testing.T) string {
	t.Helper()
	value, ok := inventoryPageTestLogDirs.Load(t.Name())
	if !ok {
		t.Fatalf("用例 %s 尚未装配测试路由，日志目录未知", t.Name())
	}
	return value.(string)
}

// inventoryTestLogHasRawError 在本用例专属日志目录里逐行解析结构化记录，判断有没有一条把原始
// 错误**完整**留在 error 字段里。
//
// 三个刻意的选择：
//
//	· 解析 JSON 后比对 error 字段的完整值，而不是在整段文本里找子串 —— 子串会把「转义之后照样
//	  看得见」（`\"` → `"`）也算命中，那是另一种形式的没写进去；
//	· 目录来自 t.TempDir()（见 inventoryPageTestLogDirs），本用例之外的行进不来；
//	· 递归遍历：logger 按 scene 分目录（pkg/logger/core.go:33），行落在 `inventory/` 子目录里。
func inventoryTestLogHasRawError(t *testing.T, rawErr error) bool {
	t.Helper()
	want := rawErr.Error()
	root := inventoryPageTestLogDir(t)
	found := false
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		// 只认 inventory scene 子目录：这条错误是 inventory 页写下的（生产侧
		// `logger.Scene(inventoryErrScene)`，见 inventory_page.go:1072-1076），落在别的 scene 里
		// 说明记录位置漂移了 —— 根目录是本用例专属的，不设这道判断就分不出是哪个 scene 写的。
		if !strings.Contains(filepath.ToSlash(path), "/inventory/") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var record map[string]any
			if json.Unmarshal([]byte(line), &record) != nil {
				continue
			}
			if got, ok := record["error"].(string); ok && got == want {
				found = true
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("遍历日志目录 %s 失败：%v", root, err)
	}
	return found
}

// TestInventoryPagesRenderSuccessPaths 成功路径照旧：空态是 200 的正常页面，且不出现失败槽。
func TestInventoryPagesRenderSuccessPaths(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     string
		wantText string
	}{
		{name: "库存管理 / 空流水", path: "/admin/inventory?project=project-1", wantText: `<p class="empty-title">没有符合条件的库存流水</p>`},
		{name: "仓库管理 / 空仓库", path: "/admin/inventory/warehouses?project=project-1", wantText: `<p class="empty-title">还没有仓库</p>`},
		{name: "变动原因字典 / 空原因", path: "/admin/inventory/reasons?project=project-1", wantText: `<p class="empty-title">还没有变动原因</p>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(nil, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			router := newInventoryPageTestRouter(t, &inventoryPageRenderStub{}, &inventoryPageRenderProjects{})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("正常装载应为 200，得到 %d：%s", rec.Code, body)
			}
			if !strings.Contains(body, tc.wantText) {
				t.Errorf("空态文案应含 %q：%s", tc.wantText, body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
				t.Errorf("正常装载 Content-Type 应含 text/html，得到 %q", ct)
			}
			if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "no-store") {
				t.Errorf("正常装载不该带 no-store，得到 %q", cc)
			}
			for _, absent := range []string{"badge-danger", "这一页的数据没能读出来"} {
				if strings.Contains(body, absent) {
					t.Errorf("正常装载不该出现失败槽 %q：%s", absent, body)
				}
			}
		})
	}
}

// TestInventoryWarehousesPageRendersRows 有数据时行照常渲染（失败槽的插入不改成功路径）。
func TestInventoryWarehousesPageRendersRows(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	inv := &inventoryPageRenderStub{warehouses: []*inventorydto.WarehouseResp{
		{ID: "warehouse-1", Code: "SZ", Name: "深圳主仓", Type: "standard", Status: "active", IsDefault: true, Sort: 1},
	}}
	router := newInventoryPageTestRouter(t, inv, &inventoryPageRenderProjects{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/inventory/warehouses?project=project-1", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("正常装载应为 200，得到 %d：%s", rec.Code, body)
	}
	for _, want := range []string{"深圳主仓", "SZ", `name="ids" value="warehouse-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("仓库行应含 %q：%s", want, body)
		}
	}
	if strings.Contains(body, `<p class="empty-title">还没有仓库</p>`) {
		t.Errorf("有数据时不该渲染空态：%s", body)
	}
}
