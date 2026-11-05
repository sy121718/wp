package feature

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

func pageToneRender(t *testing.T, name string, data map[string]any) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/t", func(c *gin.Context) { c.HTML(http.StatusOK, name, data) })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil))
	return rec.Code, rec.Body.String()
}

func pageToneRedirectData(multiHop, loop int) map[string]any {
	return map[string]any{
		"t": templates.TranslateFunc("zh-CN"), "lang": "zh-CN", "csrf_token": "tok",
		"Title": "重定向管理", "PagePath": "/api/page/redirect",
		"OkKey": "", "ErrKey": "", "Done": "", "ErrText": "",
		"CreateSource": "", "CreateTarget": "", "CreateFailed": false,
		"Projects": []any{map[string]any{"ID": "pr1", "Name": "官网"}}, "SelectedProject": "pr1", "Items": []any{},
		"Total": 0, "EffectiveCount": 0, "MultiHopCount": multiHop, "LoopCount": loop,
	}
}

func pageToneLabel(key, fallback string) string {
	return templates.TranslateFunc("zh-CN")(key, fallback)
}

func TestRedirectStatsToneFromCounts(t *testing.T) {
	cases := []struct {
		name           string
		multiHop, loop int
		multiHopTone   string
		loopTone       string
	}{
		{"两条计数都是 0 → 两处中性", 0, 0, "mute", "mute"},
		{"有多跳 → 多跳告警色、成环仍中性", 3, 0, "warn", "mute"},
		{"有成环 → 成环危险色、多跳仍中性", 0, 2, "mute", "danger"},
		{"计数为负（读不到）→ 仍中性", -1, -5, "mute", "mute"},
	}
	multiHopLabel := pageToneLabel("admin.redirect.stat.multihop", "多跳")
	loopLabel := pageToneLabel("admin.redirect.stat.loop", "成环")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := pageToneRender(t, "admin/page/page_redirects", pageToneRedirectData(tc.multiHop, tc.loop))
			if code != http.StatusOK {
				t.Fatalf("渲染重定向页失败，状态 %d", code)
			}

			if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
				t.Fatal("页面在 </html> 之前中断（模板读到缺失键或语法错误）")
			}

			wantMultiHop := fmt.Sprintf(`class="badge badge-%s">%s %d</span>`, tc.multiHopTone, multiHopLabel, tc.multiHop)
			if !strings.Contains(body, wantMultiHop) {
				t.Fatalf("多跳徽标档位不对：期望整串 %q", wantMultiHop)
			}
			wantLoop := fmt.Sprintf(`class="badge badge-%s">%s %d</span>`, tc.loopTone, loopLabel, tc.loop)
			if !strings.Contains(body, wantLoop) {
				t.Fatalf("成环徽标档位不对：期望整串 %q", wantLoop)
			}

			if tc.multiHopTone != tc.loopTone {
				for _, wrong := range []string{
					fmt.Sprintf(`class="badge badge-%s">%s %d</span>`, tc.loopTone, multiHopLabel, tc.multiHop),
					fmt.Sprintf(`class="badge badge-%s">%s %d</span>`, tc.multiHopTone, loopLabel, tc.loop),
				} {
					if strings.Contains(body, wrong) {
						t.Fatalf("徽标互相借档：出现了 %q", wrong)
					}
				}
			}

			if n := strings.Count(body, wantMultiHop); n != 1 {
				t.Fatalf("多跳徽标应恰好出现一次，实际 %d 次：%q", n, wantMultiHop)
			}
			if n := strings.Count(body, wantLoop); n != 1 {
				t.Fatalf("成环徽标应恰好出现一次，实际 %d 次：%q", n, wantLoop)
			}
		})
	}
}

func pageToneSetEngine(t *testing.T, db *gorm.DB, source, contextName, lang, engine string) {
	t.Helper()
	res := db.Exec("UPDATE sys_translation SET engine = ? WHERE source_hash = ? AND context = ? AND lang = ?",
		engine, i18n.ContentHash(source), contextName, lang)
	if res.Error != nil {
		t.Fatalf("改写译文来源失败: %v", res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("应恰好改到 1 行（source=%q context=%q lang=%q），实际 %d 行", source, contextName, lang, res.RowsAffected)
	}
}

func pageToneEngineCell(t *testing.T, body, source string) string {
	t.Helper()
	idx := strings.Index(body, "name=\"rowSource\" value=\""+source+"\"")
	if idx < 0 {
		t.Fatalf("渲染结果里找不到原文 %q 的行（模板或数据装配变了）", source)
	}
	row := body[idx:]
	if j := strings.Index(row, "</tr>"); j >= 0 {
		row = row[:j]
	}
	j := strings.LastIndex(row, "<td>")
	if j < 0 {
		t.Fatalf("原文 %q 所在行没有来源单元格", source)
	}
	return row[j:]
}

func TestTranslationEngineBadgeToneFromEngineFact(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	if router == nil {
		t.Skip("PG 不可用：来源徽标要读 sys_translation.engine")
	}

	saved := postForm(t, router, "/api/page/translations/save", url.Values{
		"pageId": {pageID}, "lang": {"en-US"},
		"rowContext": {"core.heading.text", "core.heading.subtitle", "core.button.text"},
		"rowSource":  {"关于我们", "了解更多", "联系我们"},
		"rowHash":    {i18n.ContentHash("关于我们"), i18n.ContentHash("了解更多"), i18n.ContentHash("联系我们")},
		"rowTarget":  {"About us", "Learn more", "Contact us"},
	})
	if saved.Code != http.StatusOK {
		t.Fatalf("保存译文应渲染提示页（200），实际 %d：%s", saved.Code, saved.Body.String())
	}
	pageToneSetEngine(t, db, "关于我们", "core.heading.text", "en-US", i18n.ContentEngineAI)
	pageToneSetEngine(t, db, "了解更多", "core.heading.subtitle", "en-US", i18n.ContentEnginePO)

	body := getTranslationPage(t, router, pageID, "en-US")

	for _, tc := range []struct{ source, want string }{
		{"关于我们", `<span class="badge badge-info">ai</span>`},
		{"了解更多", `<span class="badge badge-mute">po</span>`},
		{"联系我们", `<span class="badge badge-mute">manual</span>`},
	} {
		cell := pageToneEngineCell(t, body, html.EscapeString(tc.source))
		if !strings.Contains(cell, tc.want) {
			t.Fatalf("原文 %q 的来源徽标应为 %s，实际单元格：%s", tc.source, tc.want, cell)
		}
	}

	emptyCell := pageToneEngineCell(t, body, html.EscapeString("<p>我们成立于 2010 年</p>"))
	if !strings.Contains(emptyCell, `<span class="text-mute">—</span>`) {
		t.Fatalf("未翻译行的来源单元格应为中性破折号，实际：%s", emptyCell)
	}
	if strings.Contains(emptyCell, "badge-") {
		t.Fatalf("空 engine 不该渲染徽标（会显示一个没有样式的空徽标）：%s", emptyCell)
	}
}
