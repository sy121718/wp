package adminhttp_test

// admin_menus_page_test.go — 菜单管理页（树状分页）的真实渲染链路：真库 + 真 Jet 引擎。
//
// 为什么必须有这一条：模板测试用手写 map 造行数据，只能证明「模板吃得下这些字段」；
// 而这次改动的风险恰恰落在**服务端算出来的层级与可见性**上：
//   - 浏览态一页只该露出顶级行，子行带 hidden（用户看到的「只显示最上级」）；
//   - 搜索态命中子级时，祖先必须一起带出来且初始展开（否则命中行被折叠的父级挡住，
//     用户搜到了却什么都看不见）；
//   - 「上级菜单」列已删（树状列表里父级就是上一行），表头不能还留着。
//
// 走**直挂 handler** 而不是整套路由：路由带 Casbin 中间件，需要一整套策略环境，
// 而这里要验证的是「数据 → HTTP 渲染」这一段（鉴权另有既有测试覆盖）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/internal/templates"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// newMenusPageEnv 装配菜单管理页的真实链路（真 PG + 真 Jet）。
func newMenusPageEnv(t *testing.T) (*adminhttp.AdminPagesHandle, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}
	svc := adminservice.NewService(db)
	// 菜单页只用到 MenuService 与 PermService；同一个 *Service 同时实现六种契约，直接复用。
	return adminhttp.NewAdminPagesHandle(svc, svc, svc, svc, svc, svc), db
}

// serveMenusPage 渲染一次 GET /admin/menus。
func serveMenusPage(t *testing.T, handle *adminhttp.AdminPagesHandle, target string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.GET("/admin/menus", handle.MenusPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 应为 200，实际 %d", target, rec.Code)
	}
	return rec.Body.String()
}

// seedMenuTree 插一棵 root → child → grand 的树；root 的 sort_order 为负，保证落在第一页。
func seedMenuTree(t *testing.T, db *gorm.DB) (root, child, grand uint64) {
	t.Helper()
	m := adminmodel.NewMenuModel(db)
	ctx := context.Background()
	mk := func(title string, parent uint64, sort int, path string, codes ...string) uint64 {
		e := &adminmodel.MenuEntity{
			Title: title, ParentID: parent, Type: adminmodel.MenuTypeMenu,
			Path: path, Status: adminmodel.MenuStatusEnabled, SortOrder: sort,
		}
		if err := m.CreateWithPermissionCodes(ctx, e, codes); err != nil {
			t.Fatalf("插入菜单 %s 失败: %v", title, err)
		}
		return e.ID
	}
	root = mk("树用例目录", 0, -1, "")
	child = mk("树用例菜单", root, 2, "/admin/tree-probe", "treeprobe:list")
	grand = mk("树用例按钮", child, 3, "")
	return root, child, grand
}

func TestMenusPageRendersCollapsedTree(t *testing.T) {
	handle, db := newMenusPageEnv(t)
	root, child, grand := seedMenuTree(t, db)

	html := serveMenusPage(t, handle, "/admin/menus")

	if !strings.Contains(html, "data-tree-table") {
		t.Error("表格缺少树控件锚点 data-tree-table")
	}
	if strings.Contains(html, ">上级菜单</th>") {
		t.Error("树状列表里父级就是上一行，表头不该再有「上级菜单」列")
	}
	if !strings.Contains(html, `aria-level="1"`) {
		t.Error("行缺少 aria-level（读屏软件靠它判断层级）")
	}
	// 缩进是「看得出从属关系」的全部依据：顶级行不缩进，子行按 depth 递进。
	// 缩进量在 CSS 里算（theme.css 的 .tree-cell → calc(var(--tree-depth) * 1.5rem)），
	// 模板只把层级放进变量 —— 所以这里断言的是变量值，不再是内联算式
	//（docs/rules/template-boundary.md：模板不给外观，算在 CSS 里）。
	if !strings.Contains(html, "--tree-depth:0") {
		t.Error("顶级行应无缩进（--tree-depth:0）")
	}
	if !strings.Contains(html, "--tree-depth:1") {
		t.Error("子行应缩进一层（--tree-depth:1；浏览态一页里子树是完整读出来的）")
	}
	if !strings.Contains(html, "data-tree-toggle") {
		t.Error("有子行的节点应渲染折叠三角")
	}
	if !strings.Contains(html, `aria-expanded="false"`) {
		t.Error("浏览态的子行默认折叠（列表只显示最上级）")
	}
	// 三行都在同一页里（分页单位是顶级节点，子树不被切断）。
	for _, id := range []uint64{root, child, grand} {
		if !strings.Contains(html, `data-tree-id="`+strconv.FormatUint(id, 10)+`"`) {
			t.Errorf("行 %d 应在本页里（子树跟随顶级节点，不跨页切分）", id)
		}
	}
	// 子行初始隐藏 = 用户看到的那一屏只有顶级行。
	if !strings.Contains(html, " hidden>") {
		t.Error("子行应带 hidden 属性（初始不可见）")
	}
}

// TestMenusPageSearchBringsAncestors 搜索态：命中子级时带出祖先，并标出「匹配 / 上级路径」。
func TestMenusPageSearchBringsAncestors(t *testing.T) {
	handle, db := newMenusPageEnv(t)
	seedMenuTree(t, db)

	html := serveMenusPage(t, handle, "/admin/menus?keyword="+url.QueryEscape("树用例按钮"))

	for _, want := range []string{"树用例按钮", "树用例目录", "树用例菜单", "上级路径"} {
		if !strings.Contains(html, want) {
			t.Errorf("搜索态渲染缺少 %q（命中子级时必须把祖先带出来）", want)
		}
	}
	// 命中行的祖先必须初始展开：祖先折叠着的话命中行被 hidden 挡住，用户搜了等于没搜。
	if !strings.Contains(html, `aria-expanded="true"`) {
		t.Error("搜索态的祖先应初始展开")
	}
	// 页面上不报「匹配 N 条」这种计数：它既要说清 N 指什么，又与分页数不是一回事。
	if strings.Contains(html, "仅供定位") {
		t.Error("搜索态不该再输出计数提示行")
	}
}

// serveMenusUpdate 走一次真实的 POST /admin/menus/update（表单编码，非 htmx）。
func serveMenusUpdate(t *testing.T, handle *adminhttp.AdminPagesHandle, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.POST("/admin/menus/update", handle.MenusUpdate)
	req := httptest.NewRequest(http.MethodPost, "/admin/menus/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestMenusUpdateParentKeepsUneditedColumns 「只改上级菜单」的一次保存：父级变了，
// 而表单里没有编辑入口的列（组件路径 / 外链地址 / 标题键 / 隐藏与公开标记）必须原样保留。
//
// 为什么必须真发一次请求：MenuUpdate 是**全量替换**（menuUpdateColumns 把这几列显式写回），
// req 里缺失即零值落库 —— 界面上看不出任何异常，只有菜单的图标、外链、多语言标题悄悄消失。
func TestMenusUpdateParentKeepsUneditedColumns(t *testing.T) {
	handle, db := newMenusPageEnv(t)
	_, child, _ := seedMenuTree(t, db)
	m := adminmodel.NewMenuModel(db)
	ctx := context.Background()

	target := &adminmodel.MenuEntity{
		Title: "树用例目标目录", ParentID: 0, Type: adminmodel.MenuTypeDirectory,
		Status: adminmodel.MenuStatusEnabled, SortOrder: -2,
	}
	if err := m.CreateWithPermissionCodes(ctx, target, nil); err != nil {
		t.Fatalf("插入目标目录: %v", err)
	}
	// 预置「这个界面没有编辑入口」的那几列。
	// 权限码同时换成种子里真实存在的 —— validatePermissionBinding 要求每个码都存在且启用，
	// 而 seedMenuTree 用的 treeprobe:list 只是渲染用例的占位码。
	titleKey := "tree.probe.title"
	childRow, err := m.GetByID(ctx, child)
	if err != nil || childRow == nil {
		t.Fatalf("读取树用例菜单: %v", err)
	}
	childRow.ExternalURL = "https://example.test/probe"
	childRow.TitleKey = &titleKey
	childRow.IsHidden = 1
	childRow.IsPublic = 1
	if err := m.UpdateWithPermissionCodes(ctx, childRow, []string{"product:tag_list"}); err != nil {
		t.Fatalf("预置未编辑列: %v", err)
	}

	form := url.Values{
		"id": {strconv.FormatUint(child, 10)}, "title": {"树用例菜单"},
		"parent_id": {strconv.FormatUint(target.ID, 10)}, "type": {"2"}, "status": {"1"},
		"path": {"/admin/tree-probe"}, "icon": {""}, "sort_order": {"2"}, "remark": {""},
		"permission_codes": {"product:tag_list"},
	}
	rec := serveMenusUpdate(t, handle, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存成功应渲染整页提示（200，见 admin_jump.go），实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("成功提示页应带 data-jump-state=\"ok\"：%s", rec.Body.String())
	}

	row, err := m.GetByID(ctx, child)
	if err != nil || row == nil {
		t.Fatalf("回读菜单: %v", err)
	}
	if row.ParentID != target.ID {
		t.Errorf("上级菜单应改为 %d，实际 %d", target.ID, row.ParentID)
	}
	if row.ExternalURL != "https://example.test/probe" {
		t.Errorf("外链地址应原样保留，实际 %q", row.ExternalURL)
	}
	if row.TitleKey == nil || *row.TitleKey != titleKey {
		t.Errorf("标题键应原样保留，实际 %v", row.TitleKey)
	}
	if row.IsHidden != 1 || row.IsPublic != 1 {
		t.Errorf("隐藏 / 公开标记应原样保留，实际 is_hidden=%d is_public=%d", row.IsHidden, row.IsPublic)
	}
}
