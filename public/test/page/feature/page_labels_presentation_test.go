package feature

import (
	"fmt"
	"go_wp/pkg/utils"
	"strings"
	"testing"
)

func TestScheduleLabelsUseFacts(t *testing.T) {
	for _, action := range []string{"publish", "offline", "unknown"} {
		for _, status := range []string{"pending", "running", "done", "failed", "canceled", "unknown"} {
			t.Run(action+"_"+status, func(t *testing.T) {
				actionKey := "publish"
				if action == "offline" {
					actionKey = action
				}
				statusKey := status
				if status == "unknown" {
					statusKey = "pending"
				}
				code, body := pageTimePresRender(t, "fragments/page_schedule_panel", map[string]any{
					"t":         func(key, fallback string) string { return "T:" + key },
					"tf":        func(key, fallback string, _ []string) string { return "T:" + key },
					"CSRFToken": "tok", "PageID": "p", "Path": "/p", "DefaultAt": "",
					// Error / Done / Note 都是**事实**（key + 中文兜底 + %s 参数），取词在模板里做。
					"Err":   map[string]any{"Key": "", "Fallback": ""},
					"Done":  map[string]any{"Key": "", "Fallback": ""},
					"Items": []map[string]any{{"ID": 1, "Action": action, "Status": status, "Lang": "en-US", "ScheduledAt": utils.JSONTime{}, "NoteKey": "admin.page.schedule.note.controlled", "NoteFallback": "controlled error", "CanCancel": false}},
				})
				for _, want := range []string{
					"<td>T:admin.page.schedule.action." + actionKey + "</td>",
					"<td>T:admin.page.schedule.status." + statusKey + "</td>",
					// 说明列也走模板取词，交的是事实（key + 兜底）：断言 key 被送到取词函数，
					// 而不是断言一句 Go 拼好的成品串 —— 成品串会让这一列锁死在一种语言。
					`<td class="text-sm">T:admin.page.schedule.note.controlled</td>`,
				} {
					if code != 200 || strings.Count(body, want) != 1 {
						t.Fatalf("schedule cell must equal %q; status=%d", want, code)
					}
				}
			})
		}
	}
}

func TestTranslationLabelsUseFacts(t *testing.T) {
	for _, origin := range []string{"", "header", "footer", "block", "unknown"} {
		for _, count := range []int{0, 1, 6, 7} {
			t.Run(fmt.Sprintf("origin_%s_paths_%d", origin, count), func(t *testing.T) {
				paths := []string{}
				for i := 0; i < count; i++ {
					paths = append(paths, fmt.Sprintf("/p%d", i))
				}
				row := map[string]any{"Context": "core.text:text", "Component": "core.text", "Field": "text", "Source": "source", "SourceHash": "hash", "Target": "", "Engine": "", "Translated": false, "Rich": false, "Limit": 0, "Origin": origin, "ReusePages": count, "ReuseTotal": count, "ReusePaths": paths}
				data := map[string]any{"title": "translations", "menu": "pages", "t": func(key, fallback string) string { return fallback },
					// tf 是「取词 + %s 填充」：逐行错误片段（fragments/translation_errors.html）用它。
					"tf": func(key, fallback string, args []string) string { return fallback }, "PageID": "p", "PagePath": "/p", "ProjectID": "pr", "Lang": "en-US", "Langs": []any{}, "Filter": "all", "IsDefaultLang": false, "Groups": []map[string]any{{"Component": "core.text", "Rows": []map[string]any{row}}}, // Errors / SiteNote / LoadError / ParamError 一律**事实**（key + 中文兜底 + %s 参数），
					// 空 map = 无这条文案；成品串会把整页文案锁死在一种语言。
					"Errors": []any{}, "PageTotal": 1, "PageDone": 0, "SiteTotal": 1, "SiteDone": 0,
					"SiteNote": map[string]any{"Key": "", "Fallback": ""}, "RowCount": 1, "LoadFailed": false,
					"LoadError": map[string]any{"Key": "", "Fallback": ""}, "ParamInvalid": false,
					"ParamError": map[string]any{"Key": "", "Fallback": ""}}
				code, body := pageTimePresRender(t, "admin/page/page_translations", data)
				if code != 200 || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
					t.Fatalf("translation response incomplete: %d", code)
				}
				if origin == "" || origin == "unknown" {
					if !strings.Contains(body, `<td class="tr-field">text</td>`) || strings.Contains(body, `title="全站共享文本：改动后所有使用该块的页面同步生效"`) {
						t.Fatal("empty or unknown origin must omit the origin badge")
					}
				} else {
					label := map[string]string{"header": "页眉块", "footer": "页脚块", "block": "全局块"}[origin]
					want := `<span class="badge badge-info" title="全站共享文本：改动后所有使用该块的页面同步生效">` + label + `</span>`
					if strings.Count(body, want) != 1 {
						t.Fatalf("origin badge must equal %q", want)
					}
				}
				if count == 0 && strings.Contains(body, `<tr class="tr-reuse">`) {
					t.Fatal("zero reuse count must omit the reuse row")
				}
				if count > 0 {
					shown := paths
					if len(shown) > 6 {
						shown = shown[:6]
					}
					want := "<div class=\"text-mute text-sm\">\n                                该文本在 " + fmt.Sprint(count) + " 个页面出现：" + strings.Join(shown, "、")
					if count > 6 {
						want += " 等页面"
					}
					want += "<br>\n                                共 1 个译文（context = core.text:text，lang = en-US）\n                            </div>"
					if strings.Count(body, want) != 1 {
						t.Fatalf("reuse line must equal %q", want)
					}
				}
			})
		}
	}
}
