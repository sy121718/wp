// product_tag_page_query_budget_test.go — 标签页首屏查询预算与命中商品分页（审计 PERF-02）。
//
// 审计原文：标签页先 listTags，再对每个标签 GetTag 取命中商品；注释把「标签是个位数」
// 当边界，但协议没有使这个假设成立，命中数据也会一起撑大页面。整改后首屏只发
// 「工程列表 + 标签列表 + 一次批量计数」，命中商品改成展开时按页取（片段端点）。
//
// 本文件钉住三件事（都是可断言的行为，不是「看起来快了」）：
//  1. 1 / 100 / 1000 个标签时首屏 SQL 条数**完全相等**；
//  2. 首屏恰好一条命中计数语句，且它是 GROUP BY 批量聚合（不是逐标签 Count）；
//  3. 命中商品按页取，且工程边界不退化 —— 别的工程的商品即使挂着本工程的标签也不出现。
//
// 计数方式：给 gorm 会话挂一个只观察不改语义的 logger（先例见
// public/test/order/feature/order_customer_summary_window_test.go）。夹具数据用批量 SQL
// 造，避免「造 1000 个标签」本身在计数窗口里发 1000 条 SQL。
package feature

import (
	"context"
	"fmt"
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

	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"

	"go_wp/public/test/support"
)

// tagQueryCounter 记录经过 gorm 的每一条 SQL（含 Raw / Exec）。
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

// tagPageFixture 计数夹具：真实 PG + 生产迁移 + 真实 service 与 Jet 模板，
// 只把 model 换成挂了计数 logger 的会话（计数 logger 只观察，不改语义）。
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
	svc := productservice.NewService(productmodel.NewModel(counted), projects)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 页面组由 shell.PermContextMiddleware 注入权限码；这条链路不挂鉴权中间件，
	// 注入一份权限，保证页面按真实形态渲染。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{
			"product:tag_create": true, "product:tag_update": true, "product:tag_delete": true,
		})
	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	h := producthttp.NewProductPageHandle(svc, projects)
	engine.GET("/admin/product-tags", h.ProductTagsPage)
	engine.GET("/admin/product-tags/hits", h.ProductTagHitsFragment)
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

// seedTags 批量造 n 个手工标签（slug 为 tag-0000…，便于按名字判断留下了哪些）。
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

// trimTags 只留下 slug 最小的 keep 个标签（确定性裁剪，便于断言渲染的是哪一批）。
func trimTags(t *testing.T, db *gorm.DB, projectID string, keep int) {
	t.Helper()
	if err := db.Exec(
		"DELETE FROM product_tags WHERE project_id = ? AND slug NOT IN ("+
			"SELECT slug FROM product_tags WHERE project_id = ? ORDER BY slug LIMIT ?)",
		projectID, projectID, keep).Error; err != nil {
		t.Fatalf("裁剪标签失败: %v", err)
	}
}

// seedTagProducts 批量造 n 个商品，每个都挂上给定标签（tag_ids 直接写 JSONB）。
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

// productNameRe 片段里的商品名（模板把名字渲染在 <strong> 里，这里只取名字本身）。
var productNameRe = regexp.MustCompile(`商品 [0-9]{4}`)

// fragmentProductNames 取片段响应里出现的商品名（去重后排序，便于集合比较）。
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

// TestTagPageFirstPaintQueryCountIsConstant 验收核心：首屏 SQL 条数与标签数无关。
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
		// 先证明这一批标签确实渲染了（否则「SQL 不增长」可能只是因为页面是空的）。
		if !strings.Contains(body, fmt.Sprintf("标签 %04d", n-1)) {
			t.Fatalf("标签数=%d 时页面缺最后一个标签名，说明没有真正渲染这一批标签", n)
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
			// 计数必须是「一次批量聚合」：逐标签 Count 也是一条含 tag_ids 的语句，
			// 所以这里还要钉住形状 —— GROUP BY 是批量聚合的判据。
			if !strings.Contains(low, "group by") {
				t.Fatalf("命中计数必须是 GROUP BY 批量聚合，实际语句：%s", s)
			}
		}
		if hits != 1 {
			t.Fatalf("标签数=%d 时首屏命中计数语句应恰好 1 条，实际 %d 条：\n  %s", n, hits, f.counter.dump())
		}
		measured[n] = f.counter.count()
		// -v 时把实测条数与语句清单打出来：复核者不必只信断言，可以直接看首屏发了什么。
		t.Logf("标签数=%d：首屏 SQL %d 条\n  %s", n, measured[n], f.counter.dump())
	}

	base := measured[1000]
	if measured[1] != base || measured[100] != base {
		t.Fatalf("首屏 SQL 条数应不随标签数增长：1 个=%d，100 个=%d，1000 个=%d",
			measured[1], measured[100], measured[1000])
	}
	// 常数级还要「足够小」：这条防的是「每个标签不再查，但每页凭空多了十几条别的查询」。
	if base <= 0 || base > 12 {
		t.Fatalf("首屏 SQL 条数应在小常数内，实测 %d 条：\n  %s", base, f.counter.dump())
	}
}

// TestTagHitsFragmentPagesWithinProjectScope 命中商品按页取，且工程边界不退化。
//
// 两个工程各一个标签：甲工程 120 个商品、乙工程 3 个商品；再故意让乙工程的 1 个商品
// 挂着**甲工程的标签 id**（真实库里完全可能出现的越界引用，也是 RLS 换角色前唯一能靠
// 显式谓词挡住的那类脏数据）。断言：甲工程看到的命中数不多不少 120，乙工程的商品名
// 一个都不出现；用乙工程的标签 id 去查甲工程直接「标签不存在」。
func TestTagHitsFragmentPagesWithinProjectScope(t *testing.T) {
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
	// 越界引用：乙工程的商品挂着甲工程的标签 id。
	seedTagProducts(t, f.db, projectB, tagA, "越界商品", 1)

	// 首屏：只有数量与展开入口，没有商品名（PERF-02 的页面体积部分）。
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

	// 逐页取：默认每页 50 条，120 个商品 = 1/2/3 页各 50/50/20，第 4 页收敛到第 3 页。
	// 分页文案断言到「第 X-Y 条」而不是只断言页号：切片边界错了（少取一行 / 重叠一行）
	// 在只有页号的断言下看不出来。
	seen := map[string]bool{}
	for page, want := range map[int][3]int{1: {50, 1, 50}, 2: {50, 51, 100}, 3: {20, 101, 120}} {
		wantCount, from, to := want[0], want[1], want[2]
		rec := httptestGet(f.engine, fmt.Sprintf("/admin/product-tags/hits?project=%s&id=%s&page=%d", projectA, tagA, page))
		if rec.Code != http.StatusOK {
			t.Fatalf("片段应 200，实际 %d：%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		names := fragmentProductNames(body)
		if len(names) != wantCount {
			t.Fatalf("第 %d 页应有 %d 个商品，实际 %d 个", page, wantCount, len(names))
		}
		if !strings.Contains(body, fmt.Sprintf("第 %d-%d 条", from, to)) {
			t.Fatalf("第 %d 页应显示「第 %d-%d 条」的分页信息：%s", page, from, to, body)
		}
		// 工程边界：别的工程的商品名一个都不能出现（含越界挂着本标签 id 的那个）。
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

	// 越界页码不报错：回到最后一页（翻页走到头再点一下是正常操作，不该变成错误提示）。
	rec = httptestGet(f.engine, fmt.Sprintf("/admin/product-tags/hits?project=%s&id=%s&page=4", projectA, tagA))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "第 101-120 条") {
		t.Fatalf("越界页码应收敛到最后一页，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 用别的工程的标签 id 查本工程：直接「标签不存在」，不是「查到 0 条」（后者会让
	// 「这个标签是空的」与「这个标签不属于你」看起来一样）。
	rec = httptestGet(f.engine, fmt.Sprintf("/admin/product-tags/hits?project=%s&id=%s&page=1", projectA, tagB))
	if rec.Code != http.StatusOK {
		t.Fatalf("跨工程标签应回 200 片段，实际 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "标签不存在") {
		t.Fatalf("跨工程标签应提示「标签不存在」，实际：%s", body)
	}

	// 空工程（无标签、无商品）拿别的工程的标签 id 也一样：不泄露任何商品。
	projectC := f.newProject(t, "丙工程")
	rec = httptestGet(f.engine, fmt.Sprintf("/admin/product-tags/hits?project=%s&id=%s&page=1", projectC, tagA))
	if body := rec.Body.String(); !strings.Contains(body, "标签不存在") || len(fragmentProductNames(body)) != 0 {
		t.Fatalf("空工程查别的工程的标签应是「标签不存在」且不泄露任何商品：%s", body)
	}
}
