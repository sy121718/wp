package adminhttp_test

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
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

func newMenusPageEnv(t *testing.T) (*adminhttp.AdminPagesHandle, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	if err := schema.Apply(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}
	svc := adminservice.NewService(db)

	return adminhttp.NewAdminPagesHandle(svc, svc, svc, svc, svc, svc), db
}

func serveMenusPage(t *testing.T, handle *adminhttp.AdminPagesHandle, target string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(view.FailureMiddleware(view.RenderFailure))
	engine.Use(view.UseSuccess(view.NewSuccessWriter()))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/menus", view.HTMLPage, view.Slot(adminhttp.MenusPageTemplate, view.RenderPage), handle.MenusPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 应为 200，实际 %d", target, rec.Code)
	}
	return rec.Body.String()
}

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

	for _, id := range []uint64{root, child, grand} {
		if !strings.Contains(html, `data-tree-id="`+strconv.FormatUint(id, 10)+`"`) {
			t.Errorf("行 %d 应在本页里（子树跟随顶级节点，不跨页切分）", id)
		}
	}

	if !strings.Contains(html, " hidden>") {
		t.Error("子行应带 hidden 属性（初始不可见）")
	}
}

func TestMenusPageSearchBringsAncestors(t *testing.T) {
	handle, db := newMenusPageEnv(t)
	seedMenuTree(t, db)

	html := serveMenusPage(t, handle, "/admin/menus?keyword="+url.QueryEscape("树用例按钮"))

	for _, want := range []string{"树用例按钮", "树用例目录", "树用例菜单", "上级路径"} {
		if !strings.Contains(html, want) {
			t.Errorf("搜索态渲染缺少 %q（命中子级时必须把祖先带出来）", want)
		}
	}

	if !strings.Contains(html, `aria-expanded="true"`) {
		t.Error("搜索态的祖先应初始展开")
	}

	if strings.Contains(html, "仅供定位") {
		t.Error("搜索态不该再输出计数提示行")
	}
}

func serveMenusUpdate(t *testing.T, handle *adminhttp.AdminPagesHandle, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(view.FailureMiddleware(view.RenderFailure))
	engine.Use(view.UseSuccess(view.NewSuccessWriter()))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/admin/menus/update", view.HTMLFragment, handle.MenusUpdate)
	req := httptest.NewRequest(http.MethodPost, "/admin/menus/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

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
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("保存成功应返回 HTML 确认片段，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
		t.Fatalf("成功响应不得决定界面导航: %s", rec.Body.String())
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
