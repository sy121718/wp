package inventoryhttp

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

const (
	permSetKey       = "perm_set"
	buttonsKey       = "button_set"
	internalErrorKey = "MsgInternalError"
)

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

func newInventoryPageTestRouter(t *testing.T, inv inventorycontract.InventoryService,
	projects projectcontract.ProjectService, catalog ...productcontract.VariantOptionsReader) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)

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

func assertInventoryFailurePage(t *testing.T, rec *httptest.ResponseRecorder, rawErr error, emptyText string) {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("取数失败的状态码应为 500，得到 %d：%s", rec.Code, body)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON，得到 %q", ct)
	}
	for _, banned := range []string{"<!DOCTYPE", `class="admin-layout"`, "</body>", "</html>", `id="adminRail"`} {
		if strings.Contains(body, banned) {
			t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
		}
	}

	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Errorf("失败响应应带归口文案，实际：%s", body)
	}
	for _, leak := range []string{rawErr.Error(), html.EscapeString(rawErr.Error()), internalErrorKey} {
		if strings.Contains(body, leak) {
			t.Errorf("失败响应不应出现 %q：%s", leak, body)
		}
	}

	if emptyText != "" && strings.Contains(body, emptyText) {
		t.Errorf("装载失败被渲染成了空态（%q）：两档必须分开：%s", emptyText, body)
	}
}

func TestInventoryPagesLoadFailureRenderPage(t *testing.T) {

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

func TestInventoryPageLoadFailureFollowsRequestLanguage(t *testing.T) {
	rawErr := errors.New(`pq: relation "inventory_warehouses" does not exist`)

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

			if !strings.Contains(body, tc.wantMsg) {
				t.Errorf("失败响应应含归口文案 %q，实际：%s", tc.wantMsg, body)
			}
			if strings.Contains(body, internalErrorKey) {
				t.Errorf("响应里漏出了 i18n key %q：%s", internalErrorKey, body)
			}
		})
	}
}

var inventoryPageTestLogDirs sync.Map

func TestInventoryPageLoadFailureKeepsRawErrorInLog(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rawErr := errors.New(`pq: relation "inventory_movements" does not exist`)
	router := newInventoryPageTestRouter(t, &inventoryPageRenderStub{},
		&inventoryPageRenderProjects{listErr: rawErr})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/inventory", nil))

	body := rec.Body.String()
	for _, leaked := range []string{rawErr.Error(), html.EscapeString(rawErr.Error())} {
		if strings.Contains(body, leaked) {
			t.Fatalf("归口失败：原文进了页面：%s", body)
		}
	}

	if !inventoryTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}

	if inventoryTestLogHasRawError(t, errors.New(`pq: relation "never_happened" does not exist`)) {
		t.Errorf("自检失败：本次没发生过的原文也被判定为命中")
	}
}

func inventoryPageTestLogDir(t *testing.T) string {
	t.Helper()
	value, ok := inventoryPageTestLogDirs.Load(t.Name())
	if !ok {
		t.Fatalf("用例 %s 尚未装配测试路由，日志目录未知", t.Name())
	}
	return value.(string)
}

func inventoryTestLogHasRawError(t *testing.T, rawErr error) bool {
	t.Helper()
	want := rawErr.Error()
	root := inventoryPageTestLogDir(t)
	found := false
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}

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
