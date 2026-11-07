package feature

import (
	pageview "go_wp/pkg/view"
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

func pageTimePresUseSeparatedTimezones(t *testing.T) {
	t.Helper()
	oldLocal := time.Local
	time.Local = time.UTC
	t.Setenv(pageTimePresEnvKey, pageTimePresSiteTZ)
	sitetz.ResetForTest()
	t.Cleanup(func() {
		sitetz.ResetForTest()
		time.Local = oldLocal
	})
	if got := sitetz.Name(); got != pageTimePresSiteTZ {
		t.Fatalf("站点时区未生效（得到 %q，期望 %q）：时区库不可用，本组判据失去区分力", got, pageTimePresSiteTZ)
	}
}

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

func pageTimePresPageRow(id, path string) map[string]any {
	return map[string]any{
		"ID": id, "ProjectID": "p1", "Kind": "home", "DraftPath": path,
		"Active": false, "Staged": false, "Stale": false, "Version": int64(1),
		"UpdatedAt": time.Now(),
	}
}

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
	router.GET("/admin/pages", pageview.Slot(pagehttp.PagesListPageTemplate, pageview.RenderPage), handle.PagesList)
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

func TestHistoryFragmentRendersRevisionTime(t *testing.T) {
	pageTimePresUseSeparatedTimezones(t)

	at := time.Date(2026, 9, 30, 10, 0, 30, 0, time.UTC)
	code, body := pageTimePresRender(t, "fragments/history_list", map[string]any{
		"Revisions": []map[string]any{
			{"Version": int64(3), "Path": "/demo", "CreatedAt": utils.NewJSONTime(at)},
			{"Version": int64(2), "Path": "/legacy", "CreatedAt": utils.NewJSONTime(time.Time{})},
		},

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

func TestPagesReceiptBannerUsesSecondPrecision(t *testing.T) {
	pageTimePresUseSeparatedTimezones(t)

	last := utils.NewJSONTime(time.Date(2026, 9, 30, 10, 0, 7, 0, time.UTC))
	receipt := map[string]any{
		"ReceiptKnown": true, "ReceiptAlert": true, "ReceiptPending": int64(3),
		"ReceiptOldest": "2m", "ReceiptLastConverge": &last,
	}
	code, body := pageTimePresRender(t, "admin/page/pages",
		pageTimePresPagesData([]map[string]any{pageTimePresPageRow("pg1", "/demo")}, receipt))
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

	code, body = pageTimePresRender(t, "admin/page/pages",
		pageTimePresPagesData([]map[string]any{pageTimePresPageRow("pg1", "/demo")},
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
