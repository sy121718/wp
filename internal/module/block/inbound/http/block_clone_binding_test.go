package blockhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
)

type cloneBindingBlocks struct {
	blockcontract.BlockService
	req *blockdto.CloneReq
}

func (s *cloneBindingBlocks) CloneAST(_ context.Context, req *blockdto.CloneReq) (*blockdto.CloneResp, error) {
	s.req = req
	return &blockdto.CloneResp{}, nil
}

// TestBlockCloneJSONBinding 克隆是编辑器数据消费者，只绑定 JSON body。
func TestBlockCloneJSONBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"json", "application/json", `{"id":"b1"}`, http.StatusOK},
		{"form", "application/x-www-form-urlencoded", "id=b1", http.StatusBadRequest},
		{"queryCannotFillBody", "application/json", `{}`, http.StatusBadRequest},
		{"malformed", "application/json", `{"id":`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &cloneBindingBlocks{}
			h := &Handle{svc: svc}
			router := gin.New()
			router.POST("/clone", h.CloneAST)
			req := httptest.NewRequest(http.MethodPost, "/clone?id=query", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == http.StatusOK {
				if svc.req == nil || svc.req.ID != "b1" {
					t.Fatalf("克隆绑定失真：%+v", svc.req)
				}
			} else if svc.req != nil {
				t.Fatal("非法输入不应进入 service")
			}
		})
	}
}
