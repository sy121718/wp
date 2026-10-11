package masterdatahttp

// masterdata_pagination_facts_test.go — task-116（P3-B3）：变更记录页的分页消费者迁到
// pkg/pagination 事实层后的 handler 级判据。
//
// 判据是「渲染字节不变」：迁移前 handler 用 BuildPagination 预制成品（信息行句子 + 链接），
// 迁移后只交事实（pagination.Page），句子与链接由 admin/partials/pagination.html 渲染。
// 期望值用 url.Values **独立**拼出（不拿生产 helper 的结果当期望），基址必须带筛选条件 ——
// 翻页不丢条件（本页的筛选栏字段最多，最容易漏）。

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

// mdPagChanges 变更记录契约桩：只实现页面会调用的四个读方法（其余打到嵌入的 nil 接口上 panic）。
type mdPagChanges struct {
	masterdatacontract.MasterDataService
	total int64
}

func (s *mdPagChanges) ListChanges(context.Context, *masterdatadto.ListChangeReq) ([]*masterdatadto.ChangeResp, error) {
	return nil, nil
}

func (s *mdPagChanges) CountChanges(context.Context, *masterdatadto.ListChangeReq) (int64, error) {
	return s.total, nil
}

func (s *mdPagChanges) ListEntities(context.Context, *masterdatadto.ListEntityReq) ([]*masterdatadto.EntityHistoryResp, error) {
	return nil, nil
}

func (s *mdPagChanges) CountEntities(context.Context, *masterdatadto.ListEntityReq) (int64, error) {
	return 0, nil
}

// mdPagProjects 工程契约桩：只实现 List。
type mdPagProjects struct {
	projectcontract.ProjectService
}

func (mdPagProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}}, nil
}

// mdPagRender 用真实渲染器跑一次变更记录页（handler → 模板），返回响应体。
func mdPagRender(t *testing.T, changes *mdPagChanges, query string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := NewMasterDataChangePageHandle(changes, mdPagProjects{})
	engine := gin.New()
	// 模板根相对包目录：internal/module/masterdata/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	// 不注入权限上下文：本测试只钉分页事实与链接字节。
	engine.GET("/admin/masterdata/changes", handle.MasterDataChangesPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/masterdata/changes"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d，响应：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不是完整页面（缺 </html>），长度 %d", len(body))
	}
	return body
}

// mdPagNav 取页面里的分页条整块。
func mdPagNav(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<nav class="pagination"`)
	if i < 0 {
		t.Fatalf("页面里没有分页条（变更记录 45 / 每页 20 ⇒ 应有 3 页）")
	}
	j := strings.Index(body[i:], "</nav>")
	if j < 0 {
		t.Fatalf("分页条没有闭合")
	}
	return body[i : i+j+len("</nav>")]
}

// mdPagHrefs 分页条里全部 href（HTML 还原、去重、排序）。
func mdPagHrefs(nav string) []string {
	re := regexp.MustCompile(`href="([^"]*)"`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(nav, -1) {
		u := html.UnescapeString(m[1])
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// TestMasterDataChangesPaginationIsFactsDriven 链接字节与基址筛选逐条一致。
func TestMasterDataChangesPaginationIsFactsDriven(t *testing.T) {
	body := mdPagRender(t, &mdPagChanges{total: 45}, "?project=p-1&keyword=价格&page=2&limit=20")
	nav := mdPagNav(t, body)

	// FilterBaseURL 只带非空筛选：project 恒在，keyword 非空 ⇒ 两条都进基址（url.Values 字典序）。
	base := "/admin/masterdata/changes?" + url.Values{
		"project": {"p-1"}, "keyword": {"价格"},
	}.Encode()
	want := []string{base + "&page=1&limit=20", base + "&page=3&limit=20"}
	sort.Strings(want)
	if got := mdPagHrefs(nav); !reflect.DeepEqual(got, want) {
		t.Errorf("分页链接字节不符\n实际: %q\n期望: %q", got, want)
	}
	for _, want := range []string{
		`<div class="pagination-info">共 45 条，第 21-40 条</div>`,
		`<span class="pagination-btn is-active">2</span>`,
		`<a class="pagination-btn" href="` + html.EscapeString(base+"&page=1&limit=20") + `">上一页</a>`,
		`<a class="pagination-btn" href="` + html.EscapeString(base+"&page=3&limit=20") + `">下一页</a>`,
	} {
		if !strings.Contains(nav, want) {
			t.Errorf("分页条缺少整元素 %s\n实际: %s", want, nav)
		}
	}
	for _, dead := range []string{"PaginationInfo", "PaginationLinks"} {
		if strings.Contains(body, dead) {
			t.Errorf("页面里不应再出现旧键 %q", dead)
		}
	}
}

// TestMasterDataChangesSinglePageRendersNoPagination 单页不渲染分页条。
func TestMasterDataChangesSinglePageRendersNoPagination(t *testing.T) {
	body := mdPagRender(t, &mdPagChanges{total: 5}, "?project=p-1&page=1&limit=20")
	if strings.Contains(body, `<nav class="pagination"`) {
		t.Errorf("变更记录 5 条 / 每页 20 时不该渲染分页条")
	}
}
