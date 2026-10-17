package commenthttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"

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

func TestCommentReviewJSONReceipt(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		commentenums.LabelKeyDoneApproved: {"en-US": "Approved %d comments.", "zh-CN": "已通过 %d 条评论。"},
		commentenums.LabelKeyDoneRejected: {"en-US": "Rejected %d comments.", "zh-CN": "已驳回 %d 条评论。"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	for _, tc := range []struct {
		name, lang, status, want string
		affected                 int64
	}{
		{"英文通过", "en-US", "approved", "Approved 2 comments.", 2},
		{"英文驳回", "en-US", "rejected", "Rejected 1 comments.", 1},
		{"中文通过", "zh-CN", "approved", "已通过 2 条评论。", 2},
		{"零行回执", "en-US", "approved", "Approved 0 comments.", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &reviewHTTPStub{result: &commentdto.ReviewResp{Affected: tc.affected, Status: tc.status}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set("user_id", int64(17))
			c.Request = httptest.NewRequest(http.MethodPost, "/api/comment/review?lang="+tc.lang, strings.NewReader(`{"projectId":"p-1","ids":[12,13],"status":"`+tc.status+`","reviewerId":999}`))
			c.Request.Header.Set("Content-Type", "application/json")
			NewHandle(stub).Review(c)
			var got response.Response
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || got.Message != tc.want {
				t.Fatalf("status=%d message=%q want=%q", rec.Code, got.Message, tc.want)
			}
			if stub.req.ReviewerID != 17 || len(stub.req.IDs) != 2 || stub.req.IDs[0] != 12 || stub.req.Status != tc.status {
				t.Fatalf("request=%+v", stub.req)
			}
			if strings.Contains(rec.Body.String(), "skipped") {
				t.Fatal("回执凭空声明 skipped")
			}
		})
	}
}

func TestCommentReviewMessageRejectsBrokenTranslation(t *testing.T) {
	for _, tpl := range []string{"Approved %s.", "Approved %d %d.", "Approved %[2]d.", "Approved %d %s.", "Approved."} {
		t.Run(tpl, func(t *testing.T) {
			i18n.InjectForTest(map[string]map[string]string{commentenums.LabelKeyDoneApproved: {"en-US": tpl}}, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/api/comment/review?lang=en-US", nil)
			if got := commentReviewMessage(c, &commentdto.ReviewResp{Affected: 3, Status: "approved"}); got != "已通过 3 条评论。" {
				t.Fatalf("message=%q", got)
			}
		})
	}
	i18n.InjectForTest(nil, nil)
}

func TestCommentReviewJSONRejectsInvalidRequests(t *testing.T) {
	svc := commentservice.NewService(commentmodel.NewModel(nil), nil)
	for _, tc := range []struct{ name, body string }{
		{"无选择", `{"ids":[],"status":"approved"}`},
		{"超限", `{"ids":[` + strings.Repeat("1,", commentenums.MaxReviewIDs) + `1],"status":"approved"}`},
		{"非法状态", `{"ids":[1],"status":"pending"}`},
		{"字符串数组", `{"ids":["1"],"status":"approved"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/api/comment/review", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			NewHandle(svc).Review(c)
			if rec.Code != 400 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCommentReviewJSONHidesInternalError(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/api/comment/review", strings.NewReader(`{"ids":[1],"status":"approved"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	NewHandle(&reviewHTTPStub{failure: errors.New("pq: secret_relation SQLSTATE")}).Review(c)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret_relation") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
