package commentfeature

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	commentdto "go_wp/internal/module/comment/dto"
	commenthttp "go_wp/internal/module/comment/inbound/http"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"

	"github.com/gin-gonic/gin"
)

// reviewViaAPI 走真实处理器；返回值只保留调用方关心的事实，结果正文由 HTML 断言。
func reviewViaAPI(t *testing.T, svc *commentservice.Service, id int64, status string, reviewer int64) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Set("user_id", reviewer); c.Next() })
	engine.POST("/api/comment/review", commenthttp.NewHandle(svc).Review)
	form := url.Values{"projectId": {testProjectID}, "ids": {fmt.Sprint(id)}, "status": {status}}
	req := httptest.NewRequest(http.MethodPost, "/api/comment/review", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("审核 API status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

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
	router.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handler := commenthttp.NewHandle(svc)
	router.POST("/api/comment/review", func(c *gin.Context) { c.Set("user_id", int64(19)); handler.Review(c) })
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			rec := httptest.NewRecorder()
			form := url.Values{"projectId": {testProjectID}, "ids": {fmt.Sprint(submitted.ID)}, "status": {status}}
			req := httptest.NewRequest(http.MethodPost, "/api/comment/review", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			router.ServeHTTP(rec, req)
			if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "1") {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
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

func TestCommentReviewAPIRejectsEmptyAndOversizedBatches(t *testing.T) {
	handler := commenthttp.NewHandle(commentservice.NewService(commentmodel.NewModel(nil), nil))
	many := url.Values{"status": {"approved"}}
	for i := 0; i < 201; i++ {
		many.Add("ids", "1")
	}
	for _, body := range []string{url.Values{"status": {"approved"}}.Encode(), many.Encode()} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/comment/review", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handler.Review(c)
		if rec.Code != 400 {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}
