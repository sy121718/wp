package commentfeature

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	commentdto "go_wp/internal/module/comment/dto"
	commenthttp "go_wp/internal/module/comment/inbound/http"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
)

func reviewViaAPI(t *testing.T, svc *commentservice.Service, id int64, status string, reviewer int64) commentdto.ReviewResp {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("user_id", reviewer)
	body := fmt.Sprintf(`{"projectId":%q,"ids":[%d],"status":%q}`, testProjectID, id, status)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/comment/review", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	commenthttp.NewHandle(svc).Review(c)
	var receipt struct{ Data commentdto.ReviewResp }
	if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 {
		t.Fatalf("审核 API status=%d body=%s", rec.Code, rec.Body.String())
	}
	return receipt.Data
}

// Review 从生产 JSON handler 进入，核对实际写入与会话审核人。
func TestCommentReviewAPIRecordsReviewerAndReceipt(t *testing.T) {
	svc, db := setupCommentService(t)
	submitted, err := svc.Submit(context.Background(), &commentdto.SubmitReq{
		ProjectID: testProjectID, EntityType: "article", EntityID: "post-api-1", UserID: 42, Body: "审核 API 链路评论",
	})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := commenthttp.NewHandle(svc)
	router.POST("/api/comment/review", func(c *gin.Context) { c.Set("user_id", int64(19)); handler.Review(c) })
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			rec := httptest.NewRecorder()
			body := fmt.Sprintf(`{"projectId":%q,"ids":[%d],"status":%q,"reviewerId":999}`, testProjectID, submitted.ID, status)
			req := httptest.NewRequest(http.MethodPost, "/api/comment/review", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			var receipt struct {
				Code    int
				Message string
				Data    commentdto.ReviewResp
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || receipt.Data.Affected != 1 || receipt.Data.Status != status || !strings.Contains(receipt.Message, "1") {
				t.Fatalf("status=%d receipt=%+v", rec.Code, receipt)
			}
			var row commentmodel.Entity
			if err := db.Where("id = ?", submitted.ID).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			if row.Status != status || row.ReviewedBy == nil || *row.ReviewedBy != 19 || row.ReviewedAt == nil {
				t.Fatalf("row=%+v", row)
			}
		})
	}
}

// 负向 API 校验在 model 调用之前结束，使用 nil DB 证明没有数据库依赖。
func TestCommentReviewAPIRejectsEmptyAndOversizedBatches(t *testing.T) {
	handler := commenthttp.NewHandle(commentservice.NewService(commentmodel.NewModel(nil), nil))
	for _, body := range []string{`{"ids":[],"status":"approved"}`, `{"ids":[` + strings.Repeat("1,", 200) + `1],"status":"approved"}`} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/comment/review", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.Review(c)
		if rec.Code != 400 {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}
