package feature

// analytics_collect_test.go — 访问統計（BIZ-8）的接口链路测试。
//
// 覆盖点：
//   - 公开打点端点 POST /analytics/collect：合法请求 204 且真实落库（真实 PG）；
//   - 落库内容是**匿名化后**的：IP / 会话 / 访客标识都只有哈希，不出现明文；
//   - 静默失败：伪造工程 id、非法路径、超长报文一律 204 且不写库；
//   - 后台只读聚合 GET /api/analytics/summary：总数 / 按天 / 按路径与时间范围校验。
//
// 依赖 PostgreSQL（support.AcquireTestEnv 三级回退，均不可用时整体 Skip）。

import (
	"context"
	"encoding/json"
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
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const (
	collectPath = "/analytics/collect"
	summaryPath = "/api/analytics/summary"
	testPepper  = "analytics-test-pepper"
	// 真实一点的桌面 UA（服务端分类据此判定 desktop）。
	testUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36"
)

// analyticsFixture 隔离 PG schema + 生产迁移与 seed + 真实工程行 + 真实路由装配。
type analyticsFixture struct {
	engine    *gin.Engine
	svc       analyticscontract.AnalyticsService
	db        *gorm.DB
	projectID string
}

// newAnalyticsFixture 建隔离 schema 并装配统计模块（接线方式与 routers.SetupRoutes 一致：
// 公开打点直挂引擎，后台只读接口挂 /api 组）。
func newAnalyticsFixture(t *testing.T) *analyticsFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "访问统计测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	engine := gin.New()
	// 测试里不挂三层链（鉴权链路由 auth feature 覆盖），只验证参数绑定、service 与 SQL。
	svc := analyticshttp.SetupAnalyticsRoutes(engine.Group("/api"), engine, db, testPepper)
	return &analyticsFixture{engine: engine, svc: svc, db: db, projectID: project.ID}
}

// postCollect 以表单编码发一次打点请求（与 track.js 的 sendBeacon 同一形状）。
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

// viewRow page_views 的一行（断言落库内容用）。
type viewRow struct {
	Path         string
	Lang         string
	SessionID    string
	VisitorHash  string
	ReferrerHost string
	UAClass      string
	IPHash       string
}

// rows 读取本工程的全部浏览记录（按插入顺序）。
func (f *analyticsFixture) rows(t *testing.T) []viewRow {
	t.Helper()
	var out []viewRow
	if err := f.db.Table("page_views").Where("project_id = ?", f.projectID).
		Order("id ASC").Find(&out).Error; err != nil {
		t.Fatalf("读取 page_views 失败: %v", err)
	}
	return out
}

// TestAnalyticsCollectStoresAnonymizedView 合法打点 204 落库，且库里没有明文标识。
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

// TestAnalyticsCollectSilentlyDropsBadInput 伪造输入静默丢弃：一律 204、不写库。
func TestAnalyticsCollectSilentlyDropsBadInput(t *testing.T) {
	f := newAnalyticsFixture(t)
	bad := []url.Values{
		// 非法工程 id（不是 uuid）。
		{"projectId": {"not-a-uuid"}, "path": {"/a"}},
		// 工程 id 缺失。
		{"path": {"/a"}},
		// 路径不是站内绝对路径。
		{"projectId": {f.projectID}, "path": {"about"}},
		// 路径为空。
		{"projectId": {f.projectID}, "path": {""}},
	}
	for i, form := range bad {
		rec := f.postCollect(t, form, testUA)
		// 返回差异会向外部暴露「这个工程存在吗 / 这个形状会被接受吗」，所以一律 204。
		if rec.Code != http.StatusNoContent {
			t.Errorf("第 %d 个坏请求应回 204，实际 %d", i, rec.Code)
		}
	}
	if rows := f.rows(t); len(rows) != 0 {
		t.Fatalf("坏请求不该写库，实际写了 %d 行: %+v", len(rows), rows)
	}

	// 超长报文：body 被上限切断，仍然回 204 且不 panic。
	huge := url.Values{"projectId": {f.projectID}, "path": {"/" + strings.Repeat("x", 200<<10)}}
	if rec := f.postCollect(t, huge, testUA); rec.Code != http.StatusNoContent {
		t.Errorf("超长报文应回 204，实际 %d", rec.Code)
	}

	// 非表单编码的乱码 body：解析失败同样静默。
	req := httptest.NewRequest(http.MethodPost, collectPath, strings.NewReader("%zz%%%"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("乱码报文应回 204，实际 %d", rec.Code)
	}
}

// TestAnalyticsSummaryAggregates 后台只读聚合：总数 / 按天 / 按路径 / 时间范围校验。
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

	// —— 直接调契约（后台页也走这一条）——
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

	// —— 走 HTTP 只读接口 ——
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

	// 缺工程 → 400；起止颠倒 → 400 且文案来自 enums。
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

	// 别的工程查不到本工程的数据（工程维度隔离）。
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
