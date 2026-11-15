package projecthttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	pagecontract "go_wp/internal/module/page/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/schema"
)

// 工作台「主题保存」的**真实落库**闭环：生产布局 + 生产面板方法 + 真实
// SaveThemeSettings + 真实 project service + 迁移 PG。
//
// 与 workbench_panel_flow_browser_test.go 的分工：那一份的保存端点只是成功回执桩，
// 证明面板的提交/合回/迟到语义；这一份把保存端点换成生产处理器，证明
// **点一次保存，值真的进了 themes.settings，并且页面刷新被触发**。
//
// 业务服务是真实的（不是夹具）：这正是本用例唯一的验收对象。
// 测试包在 projecthttp 内部，不能引 public/test/support（support → routers →
// projecthttp 会成环），所以这里自己开隔离库并执行生产迁移。
func TestWorkbenchThemeSavePersistsInRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_WORKBENCH_PANEL_BROWSER") != "1" {
		t.Skip("设置 GOWP_WORKBENCH_PANEL_BROWSER=1 运行真实落库的主题保存闭环")
	}
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	db := newProjectHTTPTestDB(t)
	svc := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := svc.Create(contextBack(), &projectdto.CreateReq{Name: "主题落库工程"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}
	theme, err := svc.GetActiveTheme(contextBack(), project.ID)
	if err != nil || theme == nil {
		t.Fatalf("新工程应有激活主题: %v", err)
	}

	pages := &themeRefreshRecorder{}
	h := NewThemeAdminHandle(svc, pages, emptyBlockLister{})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

	engine.GET("/favicon.ico", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.GET("/static/*filepath", func(c *gin.Context) {
		name := strings.TrimPrefix(c.Param("filepath"), "/")
		body, err := templates.AssetFile(name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		contentType := "application/octet-stream"
		switch filepath.Ext(name) {
		case ".js", ".cjs":
			contentType = "text/javascript; charset=utf-8"
		case ".css":
			contentType = "text/css; charset=utf-8"
		}
		c.Data(http.StatusOK, contentType, body)
	})
	// 生产保存端点：真实处理器，失败与成功都走共享出口。
	engine.POST("/admin/themes/settings/save", pageview.HTMLFragment,
		pageview.Slot(ThemeSettingsPageTemplate, pageview.RenderPage), h.SaveThemeSettings)
	engine.POST("/workbench/global", pageview.HTMLFragment,
		pageview.Slot(GlobalPanelFragmentTemplate, pageview.RenderBare), workbenchGlobalPanel)
	engine.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(persistenceShell(theme.ID)))
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	ctx, cancel := contextWithTimeout2(120 * time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "workbench_theme_persist_browser_test.cjs")
	cmd.Env = append(os.Environ(), "GOWP_WORKBENCH_PANEL_BASE_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("主题保存真实落库闭环失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)

	// 真实落库：从数据库回读，而不是看响应。
	got, err := svc.GetTheme(contextBack(), theme.ID)
	if err != nil {
		t.Fatalf("回读主题失败: %v", err)
	}
	raw := string(got.Settings)
	if !strings.Contains(raw, "#0f766e") {
		t.Fatalf("浏览器提交的主题主色没有落库：%s", raw)
	}
	if pages.refreshed != theme.ID {
		t.Fatalf("保存后应刷新该主题页面，实际 %q", pages.refreshed)
	}
}

// persistenceShell 是工作台布局里全局面板宿主的同形外壳：表单属性与交换目标
// 写在布局里，片段与脚本来自生产源。
func persistenceShell(themeID string) string {
	return `<!DOCTYPE html><html lang="zh-CN"><head>
<meta charset="UTF-8"><meta name="csrf-token" content="flow-token"></head>
<body>
<section data-write-host id="wb-global-body">
  <p class="form-hint" data-write-status role="status" hidden></p>
  <form id="wb-global-load" data-write hx-post="/workbench/global"
        hx-trigger="wb:global-panel from:body" hx-target="#wb-global-content"
        hx-swap="innerMorph" hx-sync="this:replace" data-write-builder="workbench-global"></form>
  <div id="wb-global-content"></div>
  <div role="status" aria-live="polite" id="wb-global-save-result"></div>
</section>
<script type="application/json" id="wb-meta">{"themeId":"` + themeID + `","themeSettings":{"colors":{"primary":"#111111","text":"#333333"}},"draftPath":"/persist"}</script>
<script type="application/json" id="wb-bootstrap">{"settings":{},"root":[]}</script>
<script type="application/json" id="wb-schemas">{}</script>
<script src="/static/js/ui/htmx.min.js"></script>
<script src="/static/js/ui/write-base.js"></script>
<script src="/static/js/editor-app/panel-requests.js"></script>
<script type="module">
import { workbench } from '/static/js/editor/index.js';
import { meta } from '/static/js/editor/core.js';
import { appPanelMethods } from '/static/js/editor-app/methods/app-panels.js';
const wb = workbench(appPanelMethods);
wb.meta = meta;
wb.notice = '';
wb.saveState = 'saved';
wb.setNotice = function (text) { this.notice = text || ''; };
wb.refreshCanvas = function () {};
wb.snapshot = function () { this.saveState = 'dirty'; };
window.__wb = wb;
htmx.process(document.body);
window.__persistReady = true;
</script>
</body></html>`
}

// newProjectHTTPTestDB 建一个隔离库并执行生产迁移。
//
// 为什么不复用 support.NewMigratedPGTestDB：它在 public/test/support，而 support
// 反向 import internal/routers，routers 又 import 本包 —— 在包内用它会成环。
// 这里只做「建库 + 跑迁移」，与 support 用的是同一套迁移入口。
func newProjectHTTPTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	host := pgEnv("PGHOST", "127.0.0.1")
	user := pgEnv("PGUSER", "root")
	password := pgEnv("PGPASSWORD", "root")
	port := pgEnv("PGPORT", "5432")
	adminDSN := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		host, user, password, pgEnv("PGDATABASE", "postgres"), port)

	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{})
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatalf("取管理连接失败: %v", err)
	}
	if err := adminSQL.Ping(); err != nil {
		adminSQL.Close()
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
	}

	name := "wp_theme_persist_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		adminSQL.Close()
		t.Fatalf("建测试库失败: %v", err)
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai search_path=public,ext_shared",
		host, user, password, name, port)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + name).Error
		adminSQL.Close()
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接失败: %v", err)
	}
	t.Cleanup(func() {
		sqlDB.Close()
		_ = admin.Exec("DROP DATABASE IF EXISTS " + name).Error
		adminSQL.Close()
	})

	if err := schema.Apply(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
	}
	return db
}

// themeRefreshRecorder 记录保存后是否真的走了「刷新该主题页面」那一步。
type themeRefreshRecorder struct {
	pagecontract.PageService
	refreshed string
}

func (r *themeRefreshRecorder) RefreshThemeForTheme(_ context.Context, themeID string, _ json.RawMessage) error {
	r.refreshed = themeID
	return nil
}

func (r *themeRefreshRecorder) RefreshStructureForTheme(context.Context, string, json.RawMessage) error {
	return nil
}

func (r *themeRefreshRecorder) MarkStaleForTheme(context.Context, string) ([]string, error) {
	return nil, nil
}

// emptyBlockLister 只实现主题设置页读取块候选的那一条查询。
type emptyBlockLister struct {
	blockcontract.BlockService
}

func (emptyBlockLister) List(context.Context, *blockdto.ListReq) ([]blockdto.BlockResp, error) {
	return nil, nil
}

func contextBack() context.Context { return context.Background() }

func contextWithTimeout2(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
