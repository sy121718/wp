package feature

import (
	"context"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	mw "go_wp/pkg/middleware"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticshttp "go_wp/internal/module/analytics/inbound/http"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 页面 GET 的「无权」不是页面内提示，而是统一出口给的 403 JSON：
// 中间件 mw.PageAuthz → PageCasbinMiddlewareAs → 共享失败出口 → ErrorStatusAuto。
// 这里用真实生产迁移 + 真实 seed 库 + 真实 casbin 策略跑完整链路。
const analyticsAuthzTestUserID int64 = 4242

// authzProjects 只满足页面入口对工程列表的需求；鉴权用例不关心统计数字。
type authzProjects struct{ projectcontract.ProjectService }

func (authzProjects) List(_ context.Context) ([]projectcontract.ProjectResp, error) {
	return []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}}, nil
}

// authzFailingSummary 让 Summary 直接返回给定错误：本组用例验证出口，不验证数字。
type authzFailingSummary struct {
	analyticscontract.AnalyticsService
	err error
}

func (f authzFailingSummary) Summary(_ context.Context, _ *analyticsdto.SummaryReq) (*analyticscontract.SummaryResp, error) {
	return nil, f.err
}

// authzAnalytics 让 Summary 返回空结果：本组用例只验证出口，不验证数字。
type authzAnalytics struct {
	analyticscontract.AnalyticsService
}

func (authzAnalytics) Summary(_ context.Context, _ *analyticsdto.SummaryReq) (*analyticscontract.SummaryResp, error) {
	return &analyticscontract.SummaryResp{}, nil
}

// loadProductionGlossary 把 RunSeeds 之后真的落进 sys_i18n 的词条装进 i18n，
// 让错误出口按**生产词库**取词。
//
// 为什么要单独一步：RunSeeds 只是把词条写进库，并不等于词库已加载 —— 进程启动时
// 由 i18n 组件从库读入内存。只跑 RunSeeds 而不装载，出口会退成归口文案，
// 那样断言「声明 key 的译文上屏」是假绿。
func loadProductionGlossary(t *testing.T, db *gorm.DB) {
	t.Helper()

	var terms []struct {
		Key   string `gorm:"column:item_key"`
		Lang  string
		Value string `gorm:"column:item_value"`
	}
	if err := db.Table("sys_i18n").
		Select("item_key", "lang", "item_value").
		Where("status = ?", 1).
		Find(&terms).Error; err != nil {
		t.Fatalf("读取生产词库失败：%v", err)
	}
	data := make(map[string]map[string]string, len(terms))
	for _, term := range terms {
		if data[term.Key] == nil {
			data[term.Key] = make(map[string]string, 2)
		}
		data[term.Key][term.Lang] = term.Value
	}
	i18n.InjectForTest(data, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

func newAnalyticsAuthzEnv(t *testing.T) *gorm.DB {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}
	// 生产链路里 schema 与 seed 都跑（cmd/main.go 与 routers 装配两处都调 RunSeeds）；
	// 只跑 Run 会让权限点/菜单词条缺失，403 的判定就不是生产事实。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行业务 seed 失败: %v", err)
	}
	loadProductionGlossary(t, db)
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() { _ = pkgcasbin.Close() })
	return db
}

func newAnalyticsAuthzEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	engine.Use(func(c *gin.Context) {
		c.Next()
		for _, err := range c.Errors {
			t.Errorf("页面渲染错误: %v", err.Err)
		}
	})
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", analyticsAuthzTestUserID)
		c.Next()
	})
	engine.GET("/admin/analytics",
		mw.PageAuthz("/api/analytics/summary"),
		pageview.Slot(analyticshttp.AnalyticsPageTemplate, pageview.RenderPage),
		analyticshttp.NewAnalyticsPageHandle(authzAnalytics{}, authzProjects{}).AnalyticsPage)
	engine.GET("/admin/seo",
		mw.PageAuthzAs("/api/seo/audit", http.MethodPost),
		pageview.Slot(analyticshttp.SEOPageTemplate, pageview.RenderPage),
		analyticshttp.NewSEOPageHandle(nil, authzProjects{}).SEOPage)
	return engine
}

func TestAnalyticsPagesReturnJSON403WithoutPolicy(t *testing.T) {
	newAnalyticsAuthzEnv(t)
	engine := newAnalyticsAuthzEngine(t)

	for _, target := range []string{"/admin/analytics", "/admin/seo"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept-Language", "zh-CN")
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s：无策略时应给 403，实际 %d：%s", target, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s：403 必须是 JSON，实际 Content-Type=%q", target, ct)
		}
		body := rec.Body.String()
		if strings.Contains(body, "<html") {
			t.Errorf("%s：无权时不该渲染页面：%s", target, body)
		}
		for _, bad := range []string{"sysErrNoPerm", "ErrUserInfoMissing", "admin.err."} {
			if strings.Contains(body, bad) {
				t.Errorf("%s：不该把裸 key 交给前端（%q）：%s", target, bad, body)
			}
		}
	}
}

func TestAnalyticsPageRendersAfterPolicyGranted(t *testing.T) {
	db := newAnalyticsAuthzEnv(t)
	engine := newAnalyticsAuthzEngine(t)

	sub := strconv.FormatInt(analyticsAuthzTestUserID, 10)
	// 与 pkg/casbin/casbin.go 的 model 对齐：p = sub, obj, act, code，
	// 匹配式是 r.sub == p.sub && r.obj == p.obj && r.act == p.act，
	// 所以 v0=user_id、v1=obj、v2=act、v3=权限点，v4/v5 留空。
	if err := db.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, '/api/analytics/summary', 'GET', 'analytics:view', '', '')
		ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`, sub).Error; err != nil {
		t.Fatalf("补写策略失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/analytics?project=p-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("有策略后应 200 渲染，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "</html>") {
		t.Fatal("页面必须渲染完整（缺 </html>）")
	}
}

// 未登录（没有 user_id）是 401，不是 403：身份缺失与权限不足是两件事。
func TestAnalyticsPageWithoutIdentityReturnsJSON401(t *testing.T) {
	newAnalyticsAuthzEnv(t)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	engine.GET("/admin/analytics",
		mw.PageAuthz("/api/analytics/summary"),
		pageview.Slot(analyticshttp.AnalyticsPageTemplate, pageview.RenderPage),
		analyticshttp.NewAnalyticsPageHandle(authzAnalytics{}, authzProjects{}).AnalyticsPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/analytics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("没有身份时应给 401，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"message"`) {
		t.Errorf("401 也要给 message：%s", rec.Body.String())
	}
}

// 带 Tail 的业务错误：出口保留**传入的事实** —— 声明状态码不变，Tail 不被吞掉。
// 这里造一条 analytics 域内的带 Tail 错误（ErrDetail），只证明出口透传，不新增生产 404。
func TestAnalyticsSummaryKeepsDeclaredStatusAndTail(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		analyticsenums.ErrInvalidRange: {"zh-CN": "时间范围不合法"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/analytics/summary", analyticshttp.NewHandle(authzFailingSummary{
		err: analyticsenums.ErrDetail(analyticsenums.ErrInvalidRange, "from 晚于 to"),
	}).Summary)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/summary?projectId=p-1", nil)
	req.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("带 Tail 的业务错误仍应按声明给 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "时间范围不合法") {
		t.Errorf("声明 key 的译文要上屏：%s", body)
	}
	// 明细不进主 message：出口只把受控编码的 Tail 提取到 data.details，
	// 明文 tail 一律丢弃（见 pkg/response/errdetail.go）。这里断言的是「没混进 message」。
	if strings.Contains(body, "from 晚于 to") {
		t.Errorf("明细不该拼进主 message：%s", body)
	}
	if strings.Contains(body, analyticsenums.ErrInvalidRange) {
		t.Errorf("不该把裸 key 当 message：%s", body)
	}
}

// 状态码透传：service 用 facing.WithStatus 声明的 403 / 404，必须原样从
// **真实 Summary handler** 出去 —— 出口只认声明里的状态，不自己分类、不改判。
//
// 注意这里**不新增生产业务**：用的都是本模块已有的错误 key，只是给它挂上不同的
// 声明状态，验证「出口保留调用方传入的事实」。
func TestAnalyticsSummaryPassesThroughDeclaredStatus(t *testing.T) {
	newAnalyticsAuthzEnv(t) // 只为装载生产词库与 seed，本组用例不碰 casbin 判定

	cases := []struct {
		name   string
		status int
	}{
		{"声明 403", http.StatusForbidden},
		{"声明 404", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			engine := gin.New()

			declared := facing.WithStatus(
				analyticsenums.Err(analyticsenums.ErrInvalidParam), tc.status)
			engine.GET("/api/analytics/summary",
				analyticshttp.NewHandle(authzFailingSummary{err: declared}).Summary)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/analytics/summary?projectId=p-1", nil)
			req.Header.Set("Accept-Language", "zh-CN")
			engine.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("%s：出口应保留声明状态 %d，实际 %d：%s", tc.name, tc.status, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("%s：必须是 JSON，实际 Content-Type=%q", tc.name, ct)
			}
			body := rec.Body.String()
			if strings.Contains(body, analyticsenums.ErrInvalidParam) {
				t.Errorf("%s：不该把裸 key 当 message：%s", tc.name, body)
			}
			// 生产词库已装载，声明的 key 必须取到译文而不是归口兜底。
			if !strings.Contains(body, "请求参数无效") {
				t.Errorf("%s：应按生产词库取到 ErrInvalidParam 的译文，实际 %s", tc.name, body)
			}
			if !strings.Contains(body, `"message"`) {
				t.Errorf("%s：必须给 message：%s", tc.name, body)
			}
		})
	}
}
