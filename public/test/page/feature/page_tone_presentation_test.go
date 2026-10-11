package feature

// page_tone_presentation_test.go — page 模块「直出档位 → 事实 + 模板选档」的真实渲染证据（task-113 批次 A）。
//
// 为什么单独一批：本批把两处档位判断从 Go 挪进了模板（重定向统计的两条徽标、译文来源徽标）。
// 「删掉 Go 的 tone 字段、模板换个变量名」在编译、状态码、日志上全都看不出来 ——
// 档位算错只表现为颜色不对，而颜色正是没人会去 grep 的东西。所以这里**逐档断整属性**：
// 锚到 `class="badge badge-<档>">文案</span>` 这整串；前缀式断言（页面上别处也有 badge-warn）拦不住错档。
//
// 判据矩阵：
//   重定向统计：0/0 → 两处中性；3/0 → 多跳告警色 + 成环中性；0/2 → 反向；
//              -1/-5 → 仍中性（沿旧 `n > 0` 语义：负数是「读不到」，不是「有问题」）。
//   译文来源：ai → info；manual / po → 中性；空 engine → 不是徽标，是中性破折号。
//   （重定向计数由服务端算好（internal/module/page/service/page_redirect.go:104/107），此处只喂事实键：
//     本批判据是「模板按事实选档选得对」，不是「服务端算得对」。）

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

// pageToneRender 用真实 Jet 渲染一个后台模板，返回 (状态码, 响应体)。
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

// pageToneRedirectData 重定向页的最小完整渲染数据：键与 pages_handle.go 的 redirectPageData 一一对应
// （缺键会让 Jet 在那一行静默中断，HTTP 仍是 200 —— 所以下面每条都要断言 </html> 收尾）。
// 只喂**事实** MultiHopCount / LoopCount：档位不再由 Go 给。
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

// pageToneLabel 取**模板同源**的词条文案（t(key, fallback)），不手抄中文：
// 手抄的文案在有人改词条时会以「谁都没写错」的方式变红，那种红最后总是被改成把测试字符串也改掉。
func pageToneLabel(key, fallback string) string {
	return templates.TranslateFunc("zh-CN")(key, fallback)
}

// TestRedirectStatsToneFromCounts 重定向统计的两条徽标：档位完全由计数事实决定。
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
			// 整页收尾：Jet 读到缺失键会静默中断（状态码仍是 200，中断点之后整块消失）。
			if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
				t.Fatal("页面在 </html> 之前中断（模板读到缺失键或语法错误）")
			}
			// 整元素字节收口：颜色 + 文案 + 计数一起锚定。
			wantMultiHop := fmt.Sprintf(`class="badge badge-%s">%s %d</span>`, tc.multiHopTone, multiHopLabel, tc.multiHop)
			if !strings.Contains(body, wantMultiHop) {
				t.Fatalf("多跳徽标档位不对：期望整串 %q", wantMultiHop)
			}
			wantLoop := fmt.Sprintf(`class="badge badge-%s">%s %d</span>`, tc.loopTone, loopLabel, tc.loop)
			if !strings.Contains(body, wantLoop) {
				t.Fatalf("成环徽标档位不对：期望整串 %q", wantLoop)
			}
			// 反向：两个计数不许互相借档（成环的危险色跑到多跳上是最容易写出的错）。
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
			// 次数收口：两条统计徽标各出现一次。
			// （不做整页词表扫描：本页别处还有既有的静态 badge-warning 提示条，页面级扫描会误伤；
			//   档位判据就落在「这两条统计徽标的整串 + 次数」上。）
			if n := strings.Count(body, wantMultiHop); n != 1 {
				t.Fatalf("多跳徽标应恰好出现一次，实际 %d 次：%q", n, wantMultiHop)
			}
			if n := strings.Count(body, wantLoop); n != 1 {
				t.Fatalf("成环徽标应恰好出现一次，实际 %d 次：%q", n, wantLoop)
			}
		})
	}
}

// pageToneSetEngine 把某条译文的来源改写成指定 engine（历史数据形态：AI 批量翻译 / PO 导入）。
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

// pageToneEngineCell 取出「原文字面量所在数据行」的来源单元格（行内最后一个 <td>）。
//
// 按行取而不是整页搜：整页搜只证明「某处有个中性徽标」，分不出是哪一行的来源 ——
// 而本批最可能写错的就是「ai 那一行判成中性」。
// source 传属性里的**转义形态**（html.EscapeString），因为模板把它渲染在 value="..." 里。
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

// TestTranslationEngineBadgeToneFromEngineFact 工作台来源徽标：档位由 engine 事实决定（ai → info，其余中性）。
//
// 走真实链路（真库 → 真 handler → 真 Jet）：engine 是数据库里的字面量，
// 只有真的从 sys_translation 读出来，才能证明徽标读的是它。
func TestTranslationEngineBadgeToneFromEngineFact(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	if router == nil {
		t.Skip("PG 不可用：来源徽标要读 sys_translation.engine")
	}
	// 三条按正常路径落库（engine=manual），再把两条改写成 ai / po。
	saved := postForm(t, router, "/admin/pages/translations/save", url.Values{
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
	// 空 engine（该行还没译文）：不是徽标，是中性破折号 —— 没有来源可标，就不该摆一个徽标出来。
	emptyCell := pageToneEngineCell(t, body, html.EscapeString("<p>我们成立于 2010 年</p>"))
	if !strings.Contains(emptyCell, `<span class="text-mute">—</span>`) {
		t.Fatalf("未翻译行的来源单元格应为中性破折号，实际：%s", emptyCell)
	}
	if strings.Contains(emptyCell, "badge-") {
		t.Fatalf("空 engine 不该渲染徽标（会显示一个没有样式的空徽标）：%s", emptyCell)
	}
}
