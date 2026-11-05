package feature

import (
	"context"
	"fmt"
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"go_wp/internal/middleware/adminctx"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"

	"go_wp/public/test/support"
)

type tagQueryCounter struct {
	mu    sync.Mutex
	stmts []string
}

func (l *tagQueryCounter) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l *tagQueryCounter) Info(context.Context, string, ...interface{})     {}
func (l *tagQueryCounter) Warn(context.Context, string, ...interface{})     {}
func (l *tagQueryCounter) Error(context.Context, string, ...interface{})    {}

func (l *tagQueryCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	l.mu.Lock()
	l.stmts = append(l.stmts, sql)
	l.mu.Unlock()
}

func (l *tagQueryCounter) reset() {
	l.mu.Lock()
	l.stmts = nil
	l.mu.Unlock()
}

func (l *tagQueryCounter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.stmts)
}

func (l *tagQueryCounter) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.stmts...)
}

func (l *tagQueryCounter) dump() string {
	return strings.Join(l.all(), "\n  ")
}

type tagPageFixture struct {
	engine   *gin.Engine
	db       *gorm.DB
	projects *projectservice.Service
	counter  *tagQueryCounter
}

func newTagQueryFixture(t *testing.T) *tagPageFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	counter := &tagQueryCounter{}
	counted := db.Session(&gorm.Session{Logger: counter})
	projects := projectservice.NewService(projectmodel.NewProjectModel(counted))
	svc := productservice.NewService(productmodel.NewModel(counted), projects, masterdataservice.NewService(masterdatamodel.NewModel(counted), projects))

	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"product:tag_create": true, "product:tag_update": true, "product:tag_delete": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.tag_create": true, "product.tag_update": true, "product.tag_delete": true,
		})

	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	h := producthttp.NewProductPageHandle(svc, svc)
	engine.GET("/admin/product-tags", pageview.UseRenderer(adminui.RenderPage), h.ProductTagsPage)
	engine.GET("/api/product/tag/products", producthttp.NewProductHandle(svc).ListTagProducts)
	return &tagPageFixture{engine: engine, db: db, projects: projects, counter: counter}
}

func (f *tagPageFixture) newProject(t *testing.T, name string) string {
	t.Helper()
	p, err := f.projects.Create(context.Background(), &projectdto.CreateReq{Name: name})
	if err != nil {
		t.Fatalf("建测试工程失败: %v", err)
	}
	return p.ID
}

func seedTags(t *testing.T, db *gorm.DB, projectID string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	values := make([]string, 0, n)
	args := make([]any, 0, n*4)
	for i := 0; i < n; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		values = append(values, "(?, ?, ?, ?)")
		args = append(args, id, projectID, fmt.Sprintf("标签 %04d", i), fmt.Sprintf("tag-%04d", i))
	}
	if err := db.Exec("INSERT INTO product_tags (id, project_id, name, slug) VALUES "+strings.Join(values, ", "), args...).Error; err != nil {
		t.Fatalf("批量造标签失败: %v", err)
	}
	return ids
}

func trimTags(t *testing.T, db *gorm.DB, projectID string, keep int) {
	t.Helper()
	if err := db.Exec(
		"DELETE FROM product_tags WHERE project_id = ? AND slug NOT IN ("+
			"SELECT slug FROM product_tags WHERE project_id = ? ORDER BY slug LIMIT ?)",
		projectID, projectID, keep).Error; err != nil {
		t.Fatalf("裁剪标签失败: %v", err)
	}
}

func seedTagProducts(t *testing.T, db *gorm.DB, projectID, tagID, namePrefix string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	values := make([]string, 0, n)
	args := make([]any, 0, n*5)
	probe := fmt.Sprintf(`["%s"]`, tagID)
	for i := 0; i < n; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		values = append(values, "(?, ?, ?, ?, ?::jsonb)")
		args = append(args, id, projectID, fmt.Sprintf("%s %04d", namePrefix, i), fmt.Sprintf("p-%s-%04d", namePrefix, i), probe)
	}
	if err := db.Exec("INSERT INTO products (id, project_id, name, slug, tag_ids) VALUES "+strings.Join(values, ", "), args...).Error; err != nil {
		t.Fatalf("批量造商品失败: %v", err)
	}
	return ids
}

var productNameRe = regexp.MustCompile(`商品 [0-9]{4}`)

func fragmentProductNames(body string) []string {
	seen := map[string]bool{}
	for _, m := range productNameRe.FindAllString(body, -1) {
		seen[m] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func TestTagPageFirstPaintQueryCountIsConstant(t *testing.T) {
	f := newTagQueryFixture(t)
	if f == nil {
		return
	}
	projectID := f.newProject(t, "查询预算工程")
	seedTags(t, f.db, projectID, 1000)

	measured := map[int]int{}
	for _, n := range []int{1000, 100, 1} {
		trimTags(t, f.db, projectID, n)
		f.counter.reset()
		rec := httptestGet(f.engine, "/admin/product-tags?project="+projectID)
		if rec.Code != http.StatusOK {
			t.Fatalf("标签页应 200，实际 %d：%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()

		const perPage = 20
		wantRows := n
		if wantRows > perPage {
			wantRows = perPage
		}
		if got := strings.Count(body, `name="ids"`); got != wantRows {
			t.Fatalf("标签数=%d 时第 1 页应渲染 %d 行，实际 %d 行", n, wantRows, got)
		}
		if hasPager := strings.Contains(body, `<a class="pagination-btn" href=`); hasPager != (n > perPage) {
			t.Fatalf("标签数=%d 时分页条%s出现", n, map[bool]string{true: "应", false: "不应"}[n > perPage])
		}
		if n < 1000 && strings.Contains(body, "标签 0999") {
			t.Fatalf("标签数=%d 时不应还渲染着被裁掉的标签", n)
		}

		stmts := f.counter.all()
		hits := 0
		for _, s := range stmts {
			low := strings.ToLower(s)
			if !strings.Contains(low, "tag_ids") {
				continue
			}
			hits++

			if !strings.Contains(low, "group by") {
				t.Fatalf("命中计数必须是 GROUP BY 批量聚合，实际语句：%s", s)
			}
		}
		if hits != 1 {
			t.Fatalf("标签数=%d 时首屏命中计数语句应恰好 1 条，实际 %d 条：\n  %s", n, hits, f.counter.dump())
		}
		measured[n] = f.counter.count()

		dump := f.counter.dump()

		t.Logf("标签数=%d：首屏 SQL %d 条\n  %s", n, measured[n], dump)
		if n == 100 {
			assertTagPagesCoverAll(t, f, projectID, n)
		}
	}

	base := measured[1000]
	if measured[1] != base || measured[100] != base {
		t.Fatalf("首屏 SQL 条数应不随标签数增长：1 个=%d，100 个=%d，1000 个=%d",
			measured[1], measured[100], measured[1000])
	}

	if base <= 0 || base > 12 {
		t.Fatalf("首屏 SQL 条数应在小常数内，实测 %d 条：\n  %s", base, f.counter.dump())
	}
}

var tagNameRe = regexp.MustCompile(`标签 [0-9]{4}`)

func tagNamesOnPage(body string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 20)
	for _, m := range tagNameRe.FindAllString(body, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func assertTagPagesCoverAll(t *testing.T, f *tagPageFixture, projectID string, want int) {
	t.Helper()
	const perPage = 20
	pages := (want + perPage - 1) / perPage
	seen := map[string]bool{}
	last := map[string]bool{}
	for page := 1; page <= pages; page++ {
		f.counter.reset()
		rec := httptestGet(f.engine, fmt.Sprintf("/admin/product-tags?project=%s&page=%d", projectID, page))
		if rec.Code != http.StatusOK {
			t.Fatalf("第 %d 页应 200，实际 %d：%s", page, rec.Code, rec.Body.String())
		}
		names := tagNamesOnPage(rec.Body.String())
		wantRows := perPage
		if page == pages {
			wantRows = want - (pages-1)*perPage
		}
		if len(names) != wantRows {
			t.Fatalf("第 %d/%d 页应渲染 %d 行，实际 %d 行", page, pages, wantRows, len(names))
		}
		hits := 0
		for _, s := range f.counter.all() {
			if strings.Contains(strings.ToLower(s), "tag_ids") {
				hits++
			}
		}
		if hits != 1 {
			t.Fatalf("第 %d 页的命中计数语句应恰好 1 条，实际 %d 条（分页不应当增加它）：\n  %s",
				page, hits, f.counter.dump())
		}
		for _, name := range names {
			seen[name] = true
		}
		if page == pages {
			for _, name := range names {
				last[name] = true
			}
		}
	}
	if len(seen) != want {
		t.Fatalf("翻遍所有页只见 %d 个标签，库里是 %d 个 —— 分页把数据弄丢了", len(seen), want)
	}
	for i := 0; i < want; i++ {
		if name := fmt.Sprintf("标签 %04d", i); !seen[name] {
			t.Fatalf("%s 在任何一页都没出现 —— 分页把数据弄丢了", name)
		}
	}

	overflow := tagNamesOnPage(httptestGet(f.engine,
		fmt.Sprintf("/admin/product-tags?project=%s&page=%d", projectID, pages+1)).Body.String())
	if len(overflow) != len(last) {
		t.Fatalf("越界页码应收敛到最后一页（%d 行），实际 %d 行", len(last), len(overflow))
	}
	for _, name := range overflow {
		if !last[name] {
			t.Fatalf("越界页码返回了最后一页之外的标签 %s", name)
		}
	}
}

func TestTagProductsPagesWithinProjectScope(t *testing.T) {
	f := newTagQueryFixture(t)
	if f == nil {
		return
	}
	projectA := f.newProject(t, "甲工程")
	projectB := f.newProject(t, "乙工程")
	tagA := seedTags(t, f.db, projectA, 1)[0]
	tagB := seedTags(t, f.db, projectB, 1)[0]
	seedTagProducts(t, f.db, projectA, tagA, "商品", 120)
	seedTagProducts(t, f.db, projectB, tagB, "乙商品", 3)

	seedTagProducts(t, f.db, projectB, tagA, "越界商品", 1)

	rec := httptestGet(f.engine, "/admin/product-tags?project="+projectA)
	if rec.Code != http.StatusOK {
		t.Fatalf("标签页应 200，实际 %d", rec.Code)
	}
	pageBody := rec.Body.String()
	if names := fragmentProductNames(pageBody); len(names) != 0 {
		t.Fatalf("首屏不应内联命中商品，实际出现 %d 个商品名", len(names))
	}
	if !strings.Contains(pageBody, "120") {
		t.Fatalf("首屏应显示甲标签的命中数 120")
	}

	seen := map[string]bool{}
	for page, want := range map[int][3]int{1: {50, 1, 50}, 2: {50, 51, 100}, 3: {20, 101, 120}} {
		wantCount := want[0]
		rec := httptestGet(f.engine, fmt.Sprintf("/api/product/tag/products?projectId=%s&id=%s&page=%d", projectA, tagA, page))
		if rec.Code != http.StatusOK {
			t.Fatalf("JSON 应 200，实际 %d：%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		data := decodeTagProducts(t, rec)
		names := make([]string, 0, len(data.Items))
		for _, item := range data.Items {
			names = append(names, item.Name)
		}
		if len(names) != wantCount || data.Page != page || data.Total != 120 || data.PageSize != 50 {
			t.Fatalf("第 %d 页事实不符：%+v", page, data)
		}

		for _, foreign := range []string{"乙商品", "越界商品"} {
			if strings.Contains(body, foreign) {
				t.Fatalf("第 %d 页出现了别的工程的商品 %q", page, foreign)
			}
		}
		for _, name := range names {
			seen[name] = true
		}
	}
	if len(seen) != 120 {
		t.Fatalf("三页合计应覆盖 120 个商品，实际 %d 个（分页有重叠或漏行）", len(seen))
	}

	rec = httptestGet(f.engine, fmt.Sprintf("/api/product/tag/products?projectId=%s&id=%s&page=4", projectA, tagA))
	last := decodeTagProducts(t, rec)
	if last.Page != 3 || len(last.Items) != 20 || last.Total != 120 {
		t.Fatalf("越界页码应收敛到最后一页：%+v", last)
	}

	rec = httptestGet(f.engine, fmt.Sprintf("/api/product/tag/products?projectId=%s&id=%s&page=1", projectA, tagB))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("跨工程标签应回 JSON 404，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "标签不存在") {
		t.Fatalf("跨工程标签应提示「标签不存在」，实际：%s", body)
	}

	projectC := f.newProject(t, "丙工程")
	rec = httptestGet(f.engine, fmt.Sprintf("/api/product/tag/products?projectId=%s&id=%s&page=1", projectC, tagA))
	if body := rec.Body.String(); rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || !strings.Contains(body, "标签不存在") || len(fragmentProductNames(body)) != 0 {
		t.Fatalf("空工程查别的工程的标签应是「标签不存在」且不泄露任何商品：%s", body)
	}
}
