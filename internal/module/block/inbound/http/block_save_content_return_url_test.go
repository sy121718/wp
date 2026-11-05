package blockhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	json "go_wp/pkg/jsonx"
)

// 保存块内容的成功片段不携带版本号的 JSON 回执，也不回显调用方的 returnUrl：
// 版本由工作台从自身草稿状态推进，导航只由发起模板声明。
func TestBlockSaveContentNeverEchoesReturnURL(t *testing.T) {
	for _, returnURL := range []string{"", "/admin/blocks?page=2", "//evil.example.com/x", "https://evil.example.com/x", `/\evil.example.com`, "/admin/blocks\nevil", "/" + strings.Repeat("a", 512), `//evil.example.com/"><script>alert(1)</script>`} {
		t.Run(returnURL, func(t *testing.T) {
			svc := &stubBlockService{detail: &blockdto.BlockResp{ID: "b-1", Name: "页眉", Version: 1}}
			raw, err := json.Marshal(map[string]any{"id": "b-1", "document": json.RawMessage(`{}`), "expectedVersion": 1, "returnUrl": returnURL})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/blocks/save-content", strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			saveContentEngine(svc).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
				t.Fatalf("成功保存必须 200 且无 Location：%d %s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("成功保存应回 HTML 片段，实际 %q", ct)
			}
			for _, leak := range []string{"evil.example.com", "<script>", "/admin/blocks?page=2"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("returnUrl 不应回显：%s", rec.Body.String())
				}
			}
		})
	}
}
