package workbenchhttp

import (
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
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

func newWorkbenchBlockReturnURLRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	deps := workbenchModeDeps{blocks: &workbenchBlockStub{block: &blockdto.BlockResp{
		ID: "block-1", ProjectID: "project-1", Name: "页眉", Kind: "header", Version: 7,
		Document: json.RawMessage(`{"type":"root"}`),
	}}}
	router.GET("/workbench", pageview.Slot(CanvasLayoutTemplate, pageview.RenderPage), deps.newHandle().Workbench)
	return router
}

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

				if meta["saveBase"] != "block" {
					t.Errorf("保留 returnUrl 不该影响其它 meta 键：saveBase=%v", meta["saveBase"])
				}
			} else {
				if ok {
					t.Errorf("被拒/缺席的 returnUrl 不该进 meta，实际 %v", got)
				}

				if strings.Contains(body, "evil") {
					t.Errorf("被拒的 returnUrl 泄漏到页面：%s", body)
				}
			}
		})
	}
}

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

func TestWorkbenchBlockCarriesReadVersionAndExpectedVersionKey(t *testing.T) {
	rec := fetchWorkbenchBlock(t, newWorkbenchBlockReturnURLRouter(t), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("工作台应成功: %d %s", rec.Code, rec.Body.String())
	}
	meta := workbenchMetaSlot(t, rec.Body.String())
	if meta["version"] != float64(7) {
		t.Fatalf("必须透传读取到的版本7: %v", meta)
	}
	target, ok := meta["target"].(map[string]any)
	if !ok {
		t.Fatalf("缺目标描述符: %v", meta)
	}
	spec, ok := target["saveBody"].(map[string]any)
	if !ok || spec["versionKey"] != "expectedVersion" {
		t.Fatalf("区块必须按expectedVersion保存: %v", target)
	}
}
