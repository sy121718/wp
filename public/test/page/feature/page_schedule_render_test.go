package feature

// page_schedule_render_test.go — 定时上下线（PIPE-7）的**真实渲染证据**。
//
// 为什么单独一批：Jet 的渲染失败**不是编译期问题**，而且两种失败形态都很隐蔽 ——
//   · 用在 if 条件 / 点号输出的键缺失 → 渲染中断 → 500（buffer 里的半截内容被丢弃）；
//   · 取词函数 t 缺失 → **静默**：结构完整、状态码 200、日志干净，只有文案整片空白。
// 因此「改了模板 + 编译通过」什么也证明不了，必须在真实渲染路径上读一次响应体。
//
// 三条判据：
//   1. 面板片段走真实 handler（字段来自数据库）渲染出 200 + 页面路径 + 排定行；
//   2. 页面列表在带排定投影时渲染出「已排定 + 到点时刻」与「排定失败」徽标，且整页完整；
//   3. 列表页的**缺键**情况（直接渲染模板的单测不带排定键）模板不炸 —— 可选键一律 isset 包裹。

import (
	"context"
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	pagedto "go_wp/internal/module/page/dto"
	pagehttp "go_wp/internal/module/page/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

// TestPageSchedulePanelRendersFromDB 面板片段：真实 handler → 真实 service → 真实数据库。
func TestPageSchedulePanelRendersFromDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projectID := newPageService(t)
	scheduled := scheduleAtFuture()
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched-panel", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	if _, err = svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish",
		ScheduledAt: scheduled,
	}); err != nil {
		t.Fatalf("排定失败: %v", err)
	}
	// 执行时间列现在由**模板**格式化（dateTime：本地时区、到分钟）：期望值由判据按同一口径
	// 自己算出来 —— 旧实现是 Go 侧拼好再传下来，换成模板后这条断言必须仍然成立。
	scheduledAt, perr := sitetz.ParseDateTime(scheduled)
	if perr != nil {
		t.Fatalf("解析排定时刻失败: %v", perr)
	}
	panelTimeWant := scheduledAt.Local().Format("2006-01-02 15:04")

	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := pagehttp.NewPagesAdminHandle(svc, projects, nil, nil)
	router.GET("/admin/page-schedules/panel", adminui.BareSlot(pagehttp.PageSchedulePanelTemplate), handle.SchedulePanel)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/page-schedules/panel?pageId="+created.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("面板片段渲染失败: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// ① 结构完整（片段根元素闭口）；
	// ② 取词函数真的在工作（三处文案都来自词条 + 中文兜底）；
	// ③ 页面路径来自数据库（面板标题的归属信息），不是 query 回显；
	// ④ 已存在的排定出现在列表里（字段来自 page_schedules）。
	for _, want := range []string{
		`id="page-schedule-` + created.ID + `"`,
		"定时上下线",
		"排定",
		"/sched-panel",
		"待执行",
		"上线",
		panelTimeWant, // 执行时间列：模板按 dateTime 渲染出来的钟点
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("面板片段缺少 %q，实际内容:\n%s", want, body)
		}
	}
	// 片段里必须带上 CSRF 隐藏域与当前语言取词函数的作用结果 ——
	// 少了 csrf_token，表单提交会被中间件拒绝（而页面上看不出来）。
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Fatalf("面板片段缺少 CSRF 隐藏域:\n%s", body)
	}
	// 零值守卫仍然有效：时刻缺失时不留 0001-01-01（伪造的时刻）。
	if strings.Contains(body, "0001-01-01") {
		t.Fatalf("面板出现了伪造时刻 0001-01-01:\n%s", body)
	}
}

// TestPagesListRendersScheduleBadges 列表页：带排定投影时渲染出徽标，整页完整。
func TestPagesListRendersScheduleBadges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 排定时刻现在交**事实**（时间对象），格式由模板的 siteDateTime 决定 ——
	// 原先这里塞的是 Go 已经拼好的 "2026-09-30 10:00"。构造在 time.Local 上，
	// 站点时区（未设环境变量时回退本地）因此渲染出同一个钟点。
	pendingAt := utils.NewJSONTime(time.Date(2026, 9, 30, 10, 0, 0, 0, time.Local))
	// 零值（不是缺键）：徽标必须整块不渲染 —— 零值时间渲染出来是 0001-01-01，
	// 那是一个伪造的时刻，比不显示更坏。
	zeroAt := utils.NewJSONTime(time.Time{})
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/pages", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/page/pages", map[string]any{
			"title": "页面管理", "menu": "pages", "t": templates.TranslateFunc("zh-CN"),
			"Buttons":  map[string]any{"project.create": true, "page.create": true},
			"Projects": []map[string]any{{"ID": "p1", "Name": "站点A"}},
			"Pages": []map[string]any{
				{"ID": "pg1", "ProjectID": "p1", "Kind": "home", "DraftPath": "/demo",
					"Active": true, "Staged": true, "Stale": false, "Version": int64(3), "UpdatedAt": time.Now(),
					"SchedulePendingAt": &pendingAt, "ScheduleAlert": false},
				{"ID": "pg2", "ProjectID": "p1", "Kind": "home", "DraftPath": "/draft",
					"Active": false, "Staged": false, "Stale": false, "Version": int64(1), "UpdatedAt": time.Now(),
					// 失败原因交**事实**（key + 中文兜底）：取词在模板里做，Go 不再给成品串。
					"SchedulePendingAt": &zeroAt, "ScheduleAlert": true,
					"ScheduleFailedKey": "admin.page.schedule.failure.draft_changed", "ScheduleFailedFallback": "草稿已变更，请重新构建后再发布"},
			},
		})
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页渲染失败: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("列表页响应不完整（缺 </html>）:\n%s", body)
	}
	for _, want := range []string{
		"已排定 2026-09-30 10:00</span>", ">排定失败</span>",
		`title="草稿已变更，请重新构建后再发布"`,
		"/admin/page-schedules/panel?pageId=pg1",
		"定时",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表页缺少 %q，实际内容:\n%s", want, body)
		}
	}
}

// TestPagesListToleratesMissingScheduleKeys 列表页在**没有排定键**时照常渲染。
//
// 这是「可选键一律 isset 包裹」的回归守卫：pages_list_test.go 直接渲染本模板时
// 不带 SchedulePendingAt / ScheduleAlert，若不 isset，整页会在这两行中断（500 + 丢弃半截内容）。
func TestPagesListToleratesMissingScheduleKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/pages", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/page/pages", map[string]any{
			"title": "页面管理", "menu": "pages", "t": templates.TranslateFunc("zh-CN"),
			"Buttons":  map[string]any{},
			"Projects": []map[string]any{{"ID": "p1", "Name": "站点A"}},
			"Pages": []map[string]any{
				{"ID": "pg1", "ProjectID": "p1", "Kind": "home", "DraftPath": "/demo",
					"Active": true, "Staged": false, "Stale": false, "Version": int64(1), "UpdatedAt": time.Now()},
			},
		})
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "</html>") {
		t.Fatalf("缺排定键时列表页应照常渲染: code=%d", rec.Code)
	}
}
