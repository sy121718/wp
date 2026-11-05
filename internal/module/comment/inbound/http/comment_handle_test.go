package commenthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"

	"github.com/gin-gonic/gin"
)

type reviewHTTPStub struct {
	stubCommentSvc
	req     *commentdto.ReviewReq
	result  *commentdto.ReviewResp
	failure error
}

func (s *reviewHTTPStub) Review(_ context.Context, req *commentdto.ReviewReq) (*commentdto.ReviewResp, error) {
	s.req = req
	return s.result, s.failure
}

func TestCommentReviewHTMLReceipt(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		commentenums.LabelKeyDoneApproved: {"en-US": "Approved {count} comments.", "zh-CN": "已通过 {count} 条评论。"},
		commentenums.LabelKeyDoneRejected: {"en-US": "Rejected {count} comments.", "zh-CN": "已驳回 {count} 条评论。"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	for _, tc := range []struct {
		name, status, want string
		affected           int64
	}{
		{"通过", "approved", "已通过 2 条评论。", 2},
		{"驳回", "rejected", "已驳回 1 条评论。", 1},
		{"零行回执", "approved", "已通过 0 条评论。", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &reviewHTTPStub{result: &commentdto.ReviewResp{Affected: tc.affected, Status: tc.status}}
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
			r.Use(func(c *gin.Context) { text.AttachScope(c); c.Set("user_id", int64(17)); c.Next() })
			r.POST("/api/comment/review", NewHandle(stub).Review)
			form := url.Values{"projectId": {"p-1"}, "ids": {"12", "13"}, "status": {tc.status}}
			req := httptest.NewRequest(http.MethodPost, "/api/comment/review", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%q want=%q", rec.Code, rec.Body.String(), tc.want)
			}
			if stub.req == nil || stub.req.ReviewerID != 17 || len(stub.req.IDs) != 2 || stub.req.IDs[0] != 12 || stub.req.Status != tc.status {
				t.Fatalf("request=%+v", stub.req)
			}
			if strings.Contains(rec.Body.String(), "skipped") {
				t.Fatal("回执凭空声明 skipped")
			}
		})
	}
}

func TestCommentReviewRejectsInvalidForm(t *testing.T) {
	svc := commentservice.NewService(commentmodel.NewModel(nil), nil)
	manyIDs := url.Values{"status": {"approved"}}
	for i := 0; i < commentenums.MaxReviewIDs+1; i++ {
		manyIDs.Add("ids", "1")
	}
	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"无选择", url.Values{"status": {"approved"}}},
		{"超限", manyIDs},
		{"非法状态", url.Values{"ids": {"1"}, "status": {"pending"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/api/comment/review", strings.NewReader(tc.form.Encode()))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			NewHandle(svc).Review(c)
			if rec.Code != 400 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCommentReviewHidesInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	r.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	r.POST("/api/comment/review", pageview.HTMLFragment, NewHandle(&reviewHTTPStub{failure: errors.New("pq: secret_relation SQLSTATE")}).Review)
	form := url.Values{"ids": {"1"}, "status": {"approved"}}
	req := httptest.NewRequest("POST", "/api/comment/review", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret_relation") || !strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
