package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticshttp "go_wp/internal/module/analytics/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/permission"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const (
	collectPath = "/analytics/collect"
	summaryPath = "/api/analytics/summary"
	testPepper  = "analytics-test-pepper"

	testUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36"
)

type analyticsFixture struct {
	engine    *gin.Engine
	svc       analyticscontract.AnalyticsService
	db        *gorm.DB
	projectID string
}

func newAnalyticsFixture(t *testing.T) *analyticsFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "访问统计测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	engine := gin.New()

	svc := analyticshttp.SetupAnalyticsRoutes(permission.NewRouteGroup(engine.Group("/api")), engine, db, testPepper, nil, projects)
	return &analyticsFixture{engine: engine, svc: svc, db: db, projectID: project.ID}
}

func (f *analyticsFixture) postCollect(t *testing.T, form url.Values, ua string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, collectPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	return rec
}

type viewRow struct {
	Path         string
	Lang         string
	SessionID    string
	VisitorHash  string
	ReferrerHost string
	UAClass      string
	IPHash       string
}

func (f *analyticsFixture) rows(t *testing.T) []viewRow {
	t.Helper()
	var out []viewRow
	if err := f.db.Table("page_views").Where("project_id = ?", f.projectID).
		Order("id ASC").Find(&out).Error; err != nil {
		t.Fatalf("读取 page_views 失败: %v", err)
	}
	return out
}

func TestAnalyticsCollectStoresAnonymizedView(t *testing.T) {
	f := newAnalyticsFixture(t)
	rec := f.postCollect(t, url.Values{
		"projectId": {f.projectID},
		"path":      {"/product?id=42&utm_source=ad"},
		"lang":      {"zh-CN"},
		"referrer":  {"https://ref.example.com/landing?x=1"},
		"session":   {"s1712345678"},
		"visitor":   {"v1700000000-3"},
	}, testUA)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("打点应回 204，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	rows := f.rows(t)
	if len(rows) != 1 {
		t.Fatalf("应落库 1 行，实际 %d 行", len(rows))
	}
	row := rows[0]
	if row.Path != "/product" {
		t.Errorf("路径应截掉查询串，实际 %q", row.Path)
	}
	if row.Lang != "zh-CN" {
		t.Errorf("语言应落库，实际 %q", row.Lang)
	}
	if row.UAClass != "desktop" {
		t.Errorf("UA 分类应为 desktop，实际 %q", row.UAClass)
	}
	if row.ReferrerHost != "ref.example.com" {
		t.Errorf("来源应只留域名，实际 %q", row.ReferrerHost)
	}
	if row.IPHash == "" || row.VisitorHash == "" || row.SessionID == "" {
		t.Fatalf("匿名标识应已哈希落库: %+v", row)
	}
	for _, plain := range []string{"s1712345678", "v1700000000-3", "192.0.2.1"} {
		if row.SessionID == plain || row.VisitorHash == plain || row.IPHash == plain {
			t.Errorf("落库了明文标识 %q: %+v", plain, row)
		}
	}
}

func TestAnalyticsCollectSilentlyDropsBadInput(t *testing.T) {
	f := newAnalyticsFixture(t)
	bad := []url.Values{

		{"projectId": {"not-a-uuid"}, "path": {"/a"}},

		{"path": {"/a"}},

		{"projectId": {f.projectID}, "path": {"about"}},

		{"projectId": {f.projectID}, "path": {""}},
	}
	for i, form := range bad {
		rec := f.postCollect(t, form, testUA)

		if rec.Code != http.StatusNoContent {
			t.Errorf("第 %d 个坏请求应回 204，实际 %d", i, rec.Code)
		}
	}
	if rows := f.rows(t); len(rows) != 0 {
		t.Fatalf("坏请求不该写库，实际写了 %d 行: %+v", len(rows), rows)
	}

	huge := url.Values{"projectId": {f.projectID}, "path": {"/" + strings.Repeat("x", 200<<10)}}
	if rec := f.postCollect(t, huge, testUA); rec.Code != http.StatusNoContent {
		t.Errorf("超长报文应回 204，实际 %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, collectPath, strings.NewReader("%zz%%%"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("乱码报文应回 204，实际 %d", rec.Code)
	}
}

func TestAnalyticsSummaryAggregates(t *testing.T) {
	f := newAnalyticsFixture(t)
	collect := func(path, visitor string) {
		t.Helper()
		rec := f.postCollect(t, url.Values{
			"projectId": {f.projectID}, "path": {path}, "visitor": {visitor}, "session": {"s1"},
		}, testUA)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("打点失败: %d", rec.Code)
		}
	}
	collect("/", "v1")
	collect("/", "v2")
	collect("/about", "v1")

	res, err := f.svc.Summary(context.Background(), &analyticscontract.SummaryReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("Summary 失败: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("总浏览数应为 3，实际 %d", res.Total)
	}
	if res.Visitors != 2 {
		t.Errorf("独立访客应为 2（v1 / v2），实际 %d", res.Visitors)
	}
	if len(res.Daily) != 1 || res.Daily[0].Views != 3 {
		t.Errorf("按天聚合应只有今天一行且浏览数为 3，实际 %+v", res.Daily)
	}
	if len(res.Paths) != 2 || res.Paths[0].Path != "/" || res.Paths[0].Views != 2 {
		t.Errorf("按路径聚合应以 / 开头且浏览数为 2，实际 %+v", res.Paths)
	}
	if res.PathTotal != 2 {
		t.Errorf("路径总数应为 2，实际 %d", res.PathTotal)
	}

	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, summaryPath+"?projectId="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("只读接口应回 200，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v（body=%s）", err, rec.Body.String())
	}
	if body.Data.Total != 3 {
		t.Errorf("接口返回的总浏览数应为 3，实际 %d", body.Data.Total)
	}

	rec = httptest.NewRecorder()
	f.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, summaryPath, nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("缺工程 id 应回 400，实际 %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	f.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		summaryPath+"?projectId="+f.projectID+"&from=2026-05-01&to=2026-01-01", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("起止颠倒应回 400，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), analyticsenums.ErrInvalidRange) {
		t.Errorf("起止颠倒的错误文案应来自 analytics enums，实际 %s", rec.Body.String())
	}

	other, err := f.svc.Summary(context.Background(), &analyticscontract.SummaryReq{
		ProjectID: "2f1a4c0e-0000-4000-8000-0000000000ff",
	})
	if err != nil {
		t.Fatalf("其他工程查询失败: %v", err)
	}
	if other.Total != 0 {
		t.Errorf("其他工程不该看到本工程的浏览数，实际 %d", other.Total)
	}
}
