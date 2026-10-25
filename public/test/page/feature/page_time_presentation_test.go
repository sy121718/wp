package feature

// page_time_presentation_test.go — page 展示清理四切片（S2a-1 / S2a-3 / S2b）的**真实渲染证据**。
//
// 为什么单独一批：这几个切片把「格式化」从 Go 挪进了模板，判据的形态也跟着变 ——
// 编译通过、函数存在、模板渲染成功，都不能证明「渲染出来的是不是那个时刻」。
// 尤其是时区：真实部署里本地时区与站点时区经常相同，那时把 siteDateTime 写成 dateTime
// 照样会绿；所以这里**强制两个口径分离**（本地=UTC、站点=Asia/Shanghai），让写错必红。
//
// 判据矩阵：
//   S2a-1 修订历史片段：CreatedAt 交 JSONTime 事实 → dateTime 渲染；零值渲染成空，不留 0001-01-01。
//   S2a-3 列表行徽标：SchedulePendingAt 交 JSONTime 事实 → **站点时区**渲染；零值不显示徽标。
//   S2b   发布回执状态条：ReceiptLastConverge 交 JSONTime 事实 → dateTimeSecond 秒精度；nil 不显示后缀。
//   （S2a-2 面板行由 page_schedule_render_test.go 的 DB 用例断言：那条只有真库才拿得到时刻真值）

import (
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	pagedto "go_wp/internal/module/page/dto"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

const (
	pageTimePresEnvKey   = "GO_WP_SITE_TIMEZONE"
	pageTimePresSiteTZ   = "Asia/Shanghai"
	pageTimePresUTCStamp = "2026-09-30 10:00"
)

// pageTimePresUseSeparatedTimezones 强制「本地=UTC / 站点=Asia/Shanghai」，结束还原。
//
// 禁 t.Parallel()：改的是进程级状态（time.Local 与 sitetz 的 sync.Once 缓存）。
func pageTimePresUseSeparatedTimezones(t *testing.T) {
	t.Helper()
	oldLocal := time.Local
	time.Local = time.UTC
	t.Setenv(pageTimePresEnvKey, pageTimePresSiteTZ)
	sitetz.ResetForTest()
	t.Cleanup(func() {
		sitetz.ResetForTest() // 还原（t.Setenv 自动恢复环境变量）
		time.Local = oldLocal
	})
	if got := sitetz.Name(); got != pageTimePresSiteTZ {
		t.Fatalf("站点时区未生效（得到 %q，期望 %q）：时区库不可用，本组判据失去区分力", got, pageTimePresSiteTZ)
	}
}

// pageTimePresRender 用真实 Jet 渲染一个后台模板或片段，返回 (状态码, 响应体)。
func pageTimePresRender(t *testing.T, name string, data map[string]any) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/t", func(c *gin.Context) { c.HTML(http.StatusOK, name, data) })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil))
	return rec.Code, rec.Body.String()
}

// pageTimePresPagesData 构一份能完整渲染页面列表的最小数据（只给模板真正读到的键）。
func pageTimePresPagesData(pages []map[string]any, extra map[string]any) map[string]any {
	data := map[string]any{
		"title": "页面管理", "menu": "pages", "t": templates.TranslateFunc("zh-CN"),
		"Buttons":  map[string]any{"project.create": true, "page.create": true},
		"Projects": []map[string]any{{"ID": "p1", "Name": "站点A"}},
		"Pages":    pages,
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// pageTimePresPageRow 一行页面（UpdatedAt 只求渲染成功，不在本批判据范围内）。
func pageTimePresPageRow(id, path string, pending *utils.JSONTime) map[string]any {
	return map[string]any{
		"ID": id, "ProjectID": "p1", "Kind": "home", "DraftPath": path,
		"Active": false, "Staged": false, "Stale": false, "Version": int64(1),
		"UpdatedAt": time.Now(), "SchedulePendingAt": pending, "ScheduleAlert": false,
	}
}

// TestPagesUpdatedAtFromDB uses the production schema in an isolated test database.
func TestPagesUpdatedAtFromDB(t *testing.T) {
	pageTimePresUseSeparatedTimezones(t)
	db, svc, projectID := newPageService(t)
	page, err := svc.Create(t.Context(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/updated-fact", DraftDocument: []byte(pageDocument),
	})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := pagehttp.NewPagesAdminHandle(svc, projects, nil, nil)
	router.GET("/admin/pages", adminui.PageSlot(pagehttp.PagesListPageTemplate), handle.PagesList)
	for _, tc := range []struct {
		name string
		at   time.Time
		want string
	}{
		{"local_differs_from_site", time.Date(2026, 9, 30, 10, 0, 37, 0, time.UTC), "2026-09-30 10:00"},
		{"zero_is_empty", time.Time{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := db.WithContext(t.Context()).Model(&pagemodel.PageEntity{}).Where("id = ?", page.ID).UpdateColumn("update_time", tc.at).Error; err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages?projectId="+projectID, nil))
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
				t.Fatalf("list response incomplete: status=%d", rec.Code)
			}
			want := `<td class="text-mute text-sm">` + tc.want + `</td>`
			if strings.Count(body, want) != 1 {
				t.Fatalf("UpdatedAt cell must be exactly %q", want)
			}
			if strings.Contains(body, "0001-01-01") || strings.Contains(body, ">2026-09-30 18:00</td>") {
				t.Fatal("UpdatedAt fabricated a time or used site timezone")
			}
		})
	}
}

// TestHistoryFragmentRendersRevisionTime S2a-1：修订历史片段按事实渲染提交时刻。
//
// 零值那一行是本条的第二个判据：旧实现在 Go 里用 IsZero 守卫把零值折成空串，
// 直接把时间对象交给模板后，守卫必须仍然存在 —— 否则页面上会出现 0001-01-01，
// 那是一个**伪造的时刻**，比缺字段更坏（运维会以为这条修订存在于那年）。
func TestHistoryFragmentRendersRevisionTime(t *testing.T) {
	pageTimePresUseSeparatedTimezones(t)

	at := time.Date(2026, 9, 30, 10, 0, 30, 0, time.UTC)
	code, body := pageTimePresRender(t, "fragments/history_list", map[string]any{
		"Revisions": []map[string]any{
			{"Version": int64(3), "Path": "/demo", "CreatedAt": utils.NewJSONTime(at)},
			{"Version": int64(2), "Path": "/legacy", "CreatedAt": utils.NewJSONTime(time.Time{})},
		},
		// Error 是**事实**（key + 中文兜底）：空事实 = 无错误提示；给空串会让模板的 .Error.Key 报错。
		"Error": map[string]any{"Key": "", "Fallback": ""}, "t": templates.TranslateFunc("zh-CN"),
	})
	if code != http.StatusOK {
		t.Fatalf("片段渲染失败: %d\n%s", code, body)
	}
	if !strings.Contains(body, "/demo · "+pageTimePresUTCStamp+"</span>") {
		t.Fatalf("修订行未按 dateTime 渲染提交时刻（期望 /demo · %s</span>，本地时区口径、到分钟）:\n%s", pageTimePresUTCStamp, body)
	}
	if !strings.Contains(body, "/legacy ·") {
		t.Fatalf("零值那行不该整行消失（只该时间部分为空）:\n%s", body)
	}
	if strings.Contains(body, "0001-01-01") {
		t.Fatalf("零值时间被渲染成了伪造时刻 0001-01-01:\n%s", body)
	}
}

// TestPagesListScheduleBadgeUsesSiteTimezone S2a-3：列表行「已排定」徽标按**站点时区**渲染。
//
// 同一绝对时刻：本地（UTC）是 10:00、站点（Asia/Shanghai）是 18:00。模板若退回 dateTime
// （或站点时区解析失效退回本地），断言立刻红 —— 这正是这个切片最容易写错的地方。
func TestPagesListScheduleBadgeUsesSiteTimezone(t *testing.T) {
	pageTimePresUseSeparatedTimezones(t)

	at := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	pending := utils.NewJSONTime(at)
	zero := utils.NewJSONTime(time.Time{})
	code, body := pageTimePresRender(t, "admin/page/pages", pageTimePresPagesData([]map[string]any{
		pageTimePresPageRow("pg1", "/demo", &pending),
		pageTimePresPageRow("pg2", "/draft", &zero),
	}, nil))
	if code != http.StatusOK {
		t.Fatalf("列表页渲染失败: %d", code)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatalf("列表页响应不完整（缺 </html>）:\n%s", body)
	}
	if !strings.Contains(body, "已排定 2026-09-30 18:00</span>") {
		t.Fatalf("徽标未按站点时区渲染到点时刻（期望 已排定 2026-09-30 18:00</span>）:\n%s", body)
	}
	if strings.Contains(body, "已排定 "+pageTimePresUTCStamp) {
		t.Fatalf("徽标用的是本地时区口径（%s）—— 排定时刻必须按站点时区显示:\n%s", pageTimePresUTCStamp, body)
	}
	if n := strings.Count(body, "已排定"); n != 1 {
		t.Fatalf("零值排定不该渲染出徽标（期望 1 个「已排定」，实际 %d 个）:\n%s", n, body)
	}
	// 空时刻的徽标是「零值守卫被绕过」的形态：文字在、时刻空 —— 断言里必须钉住它，
	// 否则把零值交成「零值时间对象」而不是 nil 的退化会静默通过（页面上就是一条空徽标）。
	if strings.Contains(body, "已排定 </span>") {
		t.Fatalf("零值排定渲染出了空时刻的徽标 —— 整块都不该渲染:\n%s", body)
	}
	if strings.Contains(body, "0001-01-01") {
		t.Fatalf("零值时间被渲染成了伪造时刻 0001-01-01:\n%s", body)
	}
}

// TestPagesReceiptBannerUsesSecondPrecision S2b：回执状态条的收敛时刻到秒，nil 时整段后缀消失。
//
// 到秒是刻意的（截到分钟会让人对不上日志），用**本地**时区也是刻意的（观测值口径，不是
// 面向人的时刻口径）—— 两条都钉在这里：站点时区的 18:00:07 出现即说明口径被换错了。
func TestPagesReceiptBannerUsesSecondPrecision(t *testing.T) {
	pageTimePresUseSeparatedTimezones(t)

	last := utils.NewJSONTime(time.Date(2026, 9, 30, 10, 0, 7, 0, time.UTC))
	receipt := map[string]any{
		"ReceiptKnown": true, "ReceiptAlert": true, "ReceiptPending": int64(3),
		"ReceiptOldest": "2m", "ReceiptLastConverge": &last,
	}
	code, body := pageTimePresRender(t, "admin/page/pages",
		pageTimePresPagesData([]map[string]any{pageTimePresPageRow("pg1", "/demo", nil)}, receipt))
	if code != http.StatusOK {
		t.Fatalf("列表页渲染失败: %d", code)
	}
	if !strings.Contains(body, "待收敛发布回执") {
		t.Fatalf("回执警告条没渲染:\n%s", body)
	}
	if !strings.Contains(body, "最近一次收敛 2026-09-30 10:00:07\n") {
		t.Fatalf("收敛时刻未按 dateTimeSecond（本地时区 + 秒精度）渲染:\n%s", body)
	}
	if strings.Contains(body, "18:00:07") {
		t.Fatalf("收敛时刻用了站点时区口径 —— 观测值与面向人的时刻不是同一个口径:\n%s", body)
	}

	// 观测没读到收敛时刻（observer 缺失 / 拉取失败 / 零值）→ 后缀整段不显示，
	// 而不是渲染成空时刻或 0001-01-01。
	code, body = pageTimePresRender(t, "admin/page/pages",
		pageTimePresPagesData([]map[string]any{pageTimePresPageRow("pg1", "/demo", nil)},
			map[string]any{
				"ReceiptKnown": true, "ReceiptAlert": true, "ReceiptPending": int64(3),
				"ReceiptOldest": "2m", "ReceiptLastConverge": (*utils.JSONTime)(nil),
			}))
	if code != http.StatusOK {
		t.Fatalf("列表页渲染失败（nil 收敛时刻）: %d", code)
	}
	if !strings.Contains(body, "待收敛发布回执") {
		t.Fatalf("nil 收敛时刻不该把整条警告吞掉:\n%s", body)
	}
	if strings.Contains(body, "最近一次收敛") {
		t.Fatalf("收敛时刻为 nil 时不该渲染该后缀:\n%s", body)
	}
	if strings.Contains(body, "0001-01-01") {
		t.Fatalf("nil 收敛时刻被渲染成了伪造时刻 0001-01-01:\n%s", body)
	}
}
