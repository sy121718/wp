package workbenchhttp

// workbench_block_return_url_test.go — 全局块编辑画布的 returnUrl（GET /workbench?block=ID&returnUrl=…）
// 在**真实 HTTP 渲染输出**上的行为（task-80）。
//
// 工作台在这条链路上是**搬运方**：workbench_page.go 只在 safepath.LocalReturnPath 认下的值上
// 把 returnUrl 写进 meta（`<script type="application/json" id="wb-meta">`），非法值与没传
// 对用户是同一件事 —— meta 里没有这个键，保存后留在工作台。真正决定跳不跳的是消费侧
// SaveBlockContent（见 internal/module/block/inbound/http/*_return_url_test.go）。
//
// 所以这里断言的是可观测输出：200 + wb-meta 槽里的 returnUrl 键在/不在（按 JSON 解析，
// 不靠字符串猜测），并且被拒的值一个字符都不许出现在整页正文里。

import (
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/safepath"

	"github.com/gin-gonic/gin"
)

// newWorkbenchBlockReturnURLRouter 装配与线上同一条渲染路径的测试路由（不依赖数据库）。
// 只有块画布模式需要的能力被注入：BlockByID → blocks.Detail。
func newWorkbenchBlockReturnURLRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	deps := workbenchModeDeps{blocks: &workbenchBlockStub{block: &blockdto.BlockResp{
		ID: "block-1", ProjectID: "project-1", Name: "页眉", Kind: "header",
		Document: json.RawMessage(`{"type":"root"}`),
	}}}
	router.GET("/workbench", adminui.PageSlot(CanvasLayoutTemplate), deps.newHandle().Workbench)
	return router
}

// workbenchMetaSlot 取 wb-meta 槽（`<script type="application/json" id="wb-meta">`）里的 JSON。
//
// 按结构解析而不是扫字符串：meta 是 `utils.JsonSafe` 原样输出的 JSON，
// 只有解析后才能区分「键存在且值正确」与「页面上某处碰巧出现过这个词」。
func workbenchMetaSlot(t *testing.T, body string) map[string]any {
	t.Helper()
	const marker = `id="wb-meta">`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("没找到工作台 meta 槽（模板结构变了？）：%s", body)
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatalf("meta 槽没有闭合的 </script>：%s", body)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(rest[:j]), &meta); err != nil {
		t.Fatalf("meta 槽不是合法 JSON（%v）：%s", err, rest[:j])
	}
	return meta
}

// fetchWorkbenchBlock 用真实 GET 打块画布；returnURL 为 nil 表示整个查询参数缺席。
func fetchWorkbenchBlock(t *testing.T, router *gin.Engine, returnURL *string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/workbench?block=block-1&lang=zh-CN"
	if returnURL != nil {
		target += "&returnUrl=" + url.QueryEscape(*returnURL)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	router.ServeHTTP(rec, req)
	return rec
}

// TestWorkbenchBlockReturnURLWhitelist 白名单在块画布渲染输出上的真实表现。
func TestWorkbenchBlockReturnURLWhitelist(t *testing.T) {
	atLimit := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes-1)
	overLimit := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes)

	cases := []struct {
		name      string
		returnURL *string
		wantKeep  bool
	}{
		{"站内相对路径保留", strPtr("/admin/blocks?page=2&kind=header"), true},
		{"恰好 512 字节保留", strPtr(atLimit), true},
		{"协议相对 URL 丢弃", strPtr("//evil.example.com/x"), false},
		{"绝对 URL 丢弃", strPtr("https://evil.example.com/x"), false},
		{"反斜杠丢弃", strPtr(`/\evil.example.com`), false},
		{"含换行丢弃", strPtr("/admin/blocks\nevil"), false},
		{"含制表符丢弃", strPtr("/admin/blocks\tevil"), false},
		{"超过 512 字节丢弃", strPtr(overLimit), false},
		{"缺前导斜杠丢弃", strPtr("admin/blocks?evil"), false},
		{"空串等价于没传", strPtr(""), false},
		{"参数缺席", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := fetchWorkbenchBlock(t, newWorkbenchBlockReturnURLRouter(t), tc.returnURL)
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码应为 200，实际 %d（响应体：%s）", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			meta := workbenchMetaSlot(t, body)

			got, ok := meta["returnUrl"]
			if tc.wantKeep {
				if !ok {
					t.Fatalf("meta 里应有 returnUrl，实际没有（meta=%v）", meta)
				}
				if got != *tc.returnURL {
					t.Errorf("meta.returnUrl 应为 %q，实际 %v", *tc.returnURL, got)
				}
				// 白名单是「按值收敛」，不是「整键删掉」：其它搬运字段必须原样还在。
				if meta["saveBase"] != "block" {
					t.Errorf("保留 returnUrl 不该影响其它 meta 键：saveBase=%v", meta["saveBase"])
				}
			} else {
				if ok {
					t.Errorf("被拒/缺席的 returnUrl 不该进 meta，实际 %v", got)
				}
				// 被拒的值不许在整页任何位置出现（meta 只是最可能的一处）。
				if strings.Contains(body, "evil") {
					t.Errorf("被拒的 returnUrl 泄漏到页面：%s", body)
				}
			}
		})
	}
}

// TestWorkbenchBlockReturnURLRejectionIsNotEchoed 被拒值不许被回显到渲染输出。
//
// 方向相反的错法：把非法值照样拼进 meta（前端拿去跳转），或以提示语形式写进页面。
// 这里用带 HTML/脚本注入形态的值，确认页面上连片段都没有。
func TestWorkbenchBlockReturnURLRejectionIsNotEchoed(t *testing.T) {
	const hostile = `//evil.example.com/"><script>alert(1)</script>`
	rec := fetchWorkbenchBlock(t, newWorkbenchBlockReturnURLRouter(t), strPtr(hostile))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	meta := workbenchMetaSlot(t, body)
	if _, ok := meta["returnUrl"]; ok {
		t.Errorf("被拒的 returnUrl 不该进 meta：%v", meta["returnUrl"])
	}
	for _, leak := range []string{"evil.example.com", "alert(1)"} {
		if strings.Contains(body, leak) {
			t.Errorf("页面不该含被拒 returnUrl 的片段 %q", leak)
		}
	}
}

func strPtr(s string) *string { return &s }
