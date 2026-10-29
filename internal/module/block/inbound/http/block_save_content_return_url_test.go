package blockhttp

// 保存端点只交JSON事实；returnUrl由工作台前端消费，服务端不导航或回显。
import (
	"github.com/gin-gonic/gin"
	blockdto "go_wp/internal/module/block/dto"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlockSaveContentAlwaysReturnsVersionedJSON(t *testing.T) {
	for _, returnURL := range []string{"", "/admin/blocks?page=2", "//evil.example.com/x", "https://evil.example.com/x", `/\evil.example.com`, "/admin/blocks\nevil", "/" + strings.Repeat("a", 512), `//evil.example.com/"><script>alert(1)</script>`} {
		t.Run(returnURL, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			svc := &stubBlockService{detail: &blockdto.BlockResp{ID: "b-1", Name: "页眉", Version: 1}}
			router := gin.New()
			router.POST("/admin/blocks/save-content", NewBlockPageHandle(svc, nil).SaveBlockContent)
			raw, err := json.Marshal(map[string]any{"id": "b-1", "document": json.RawMessage(`{}`), "expectedVersion": 1, "returnUrl": returnURL})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/admin/blocks/save-content", strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
				t.Fatalf("成功保存必须200 JSON且无Location: %d %s", rec.Code, rec.Body.String())
			}
			var response struct {
				Code int `json:"code"`
				Data struct {
					Version int64 `json:"version"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || response.Data.Version != 2 {
				t.Fatalf("保存回执必须携新版本: %s", rec.Body.String())
			}
			for _, leak := range []string{"evil.example.com", "<script>", "/admin/blocks?page=2"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("returnUrl不应回显: %s", rec.Body.String())
				}
			}
		})
	}
}
