package feature

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newDepartmentMenuPageEngine(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	svc := adminservice.NewService(db)
	// perms 也要给：菜单页在渲染时取权限点候选（菜单绑定权限点的复选清单，迁移 470），
	// 少传一个契约会让那一页在渲染期 panic（nil 接口调用）。
	h := adminhttp.NewAdminPagesHandle(nil, nil, svc, svc, svc, nil)
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath: support.NewComponentTestConfig(t), GinMode: gin.TestMode, InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin/departments", h.DepartmentsPage)
			e.GET("/admin/menus", h.MenusPage)
		},
	})
	if err != nil {
		t.Fatalf("初始化页面测试依赖: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

func fetchDepartmentMenuPage(t *testing.T, engine *gin.Engine, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "</html>") {
		t.Fatalf("GET %s: status=%d body=%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func pageTableBody(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<tbody>")
	end := strings.Index(body, "</tbody>")
	if start < 0 || end <= start {
		t.Fatal("页面缺少完整列表 tbody")
	}
	return body[start:end]
}

func TestAdminDepartmentMenuPagePagination(t *testing.T) {
	for _, tc := range []struct {
		name, path, prefix, parent string
		seed                       func(*testing.T, *gorm.DB)
	}{
		{
			name: "部门", path: "/admin/departments", prefix: "分页部门", parent: "分页部门00",
			seed: func(t *testing.T, db *gorm.DB) {
				t.Helper()
				var root adminmodel.DeptEntity
				for i := 0; i < 5; i++ {
					row := adminmodel.DeptEntity{DeptName: fmt.Sprintf("分页部门%02d", i), DeptCode: fmt.Sprintf("page-dept-%02d", i), Ancestors: "0", Status: 1, SortOrder: i}
					if i > 0 {
						row.ParentID = root.ID
						row.Ancestors = fmt.Sprintf("0,%d", root.ID)
					}
					if err := db.Create(&row).Error; err != nil {
						t.Fatalf("插入部门: %v", err)
					}
					if i == 0 {
						root = row
					}
				}
			},
		},
		{
			name: "菜单", path: "/admin/menus", prefix: "分页菜单", parent: "分页菜单00",
			seed: func(t *testing.T, db *gorm.DB) {
				t.Helper()
				var root adminmodel.MenuEntity
				for i := 0; i < 5; i++ {
					row := adminmodel.MenuEntity{Title: fmt.Sprintf("分页菜单%02d", i), Type: adminmodel.MenuTypeDirectory, Status: 1, SortOrder: i}
					if i > 0 {
						row.ParentID = root.ID
					}
					if err := db.Create(&row).Error; err != nil {
						t.Fatalf("插入菜单: %v", err)
					}
					if i == 0 {
						root = row
					}
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := support.NewMigratedPGTestDB(t)
			if tc.path == "/admin/menus" {
				if err := db.Exec("DELETE FROM sys_menus").Error; err != nil {
					t.Fatal(err)
				}
			}
			tc.seed(t, db)
			engine := newDepartmentMenuPageEngine(t, db)
			for _, p := range []struct {
				page, first, info string
				count             int
			}{
				{"1", "00", "共 5 条，第 1-2 条", 2},
				{"2", "02", "共 5 条，第 3-4 条", 2},
				{"999", "04", "共 5 条，第 5-5 条", 1},
			} {
				body := fetchDepartmentMenuPage(t, engine, tc.path+"?page="+p.page+"&limit=2")
				rows := pageTableBody(t, body)
				if got := strings.Count(rows, "data-filter-text="); got != p.count {
					t.Fatalf("第 %s 页行数错误: %d", p.page, got)
				}
				if !strings.Contains(rows, tc.prefix+p.first) || !strings.Contains(body, p.info) {
					t.Fatalf("第 %s 页内容或总数错误: %s", p.page, rows)
				}
				if !strings.Contains(body, tc.prefix+"04</option>") {
					t.Fatal("父级下拉应包含跨页节点")
				}
				if p.page != "1" && !strings.Contains(rows, tc.parent) {
					t.Fatal("跨页子节点应显示其父级名称")
				}
				if p.page == "999" && !strings.Contains(body, `class="pagination-btn is-active">3</span>`) {
					t.Fatal("越界页应钳制到实际末页")
				}
			}

			keyword := tc.prefix + "0"
			body := fetchDepartmentMenuPage(t, engine, tc.path+"?keyword="+url.QueryEscape(keyword)+"&page=2&limit=2")
			rows := pageTableBody(t, body)
			if !strings.Contains(rows, `data-filter-text="`+tc.prefix+"02") || strings.Contains(rows, `data-filter-text="`+tc.prefix+"00") || !strings.Contains(body, "共 5 条，第 3-4 条") {
				t.Fatalf("关键词翻页应保留过滤且只返回当前两行: %s", rows)
			}
			link := regexp.MustCompile(`href="[^"]*keyword=[^"]*page=3[^"]*"`).FindString(body)
			if link == "" || !strings.Contains(link, url.QueryEscape(keyword)) {
				t.Fatalf("翻页链接丢失关键词: %q", link)
			}
			body = fetchDepartmentMenuPage(t, engine, tc.path+"?keyword="+url.QueryEscape("不存在的关键字"))
			if !strings.Contains(pageTableBody(t, body), "没有匹配") || !strings.Contains(body, "共 0") {
				t.Fatal("无匹配时应区分过滤空态和真正没有数据，标题显示匹配总数 0")
			}
			if strings.Contains(body, `class="pagination"`) || !strings.Contains(body, tc.prefix+"04</option>") {
				t.Fatal("无匹配时分页应消失，但父级下拉仍应完整")
			}
		})
	}
}
