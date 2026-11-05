package commentmcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/mcp"
	commentdto "go_wp/internal/module/comment/dto"
)

type stubReview struct {
	got *commentdto.ReviewReq
	res *commentdto.ReviewResp
	err error
}

func (s *stubReview) Review(_ context.Context, req *commentdto.ReviewReq) (*commentdto.ReviewResp, error) {
	s.got = req
	return s.res, s.err
}

type reviewStore struct{ m map[string]mcp.Result }

func (s *reviewStore) Lookup(_ context.Context, tool, key string) (mcp.Result, bool, error) {
	r, ok := s.m[tool+"|"+key]
	return r, ok, nil
}

func (s *reviewStore) Save(_ context.Context, tool, key string, res mcp.Result) error {
	s.m[tool+"|"+key] = res
	return nil
}

func mustReviewTool(t *testing.T, stub *stubReview) mcp.Tool {
	t.Helper()
	tools, err := WriteTools(stub)
	if err != nil {
		t.Fatalf("装配审核工具失败: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("审核工具应只有 1 个，实得 %d", len(tools))
	}
	return tools[0]
}

func callReview(t *testing.T, stub *stubReview, userID int64, args map[string]any) (string, error) {
	t.Helper()
	tool := mustReviewTool(t, stub)
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	ctx := context.Background()
	if userID > 0 {
		ctx = mcp.WithUserID(ctx, userID)
	}
	res, err := tool.Invoke(ctx, raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func baseReviewArgs() map[string]any {
	return map[string]any{"projectId": "p-1", "ids": []int64{11, 12}, "status": "approved"}
}

func TestCommentReviewRejectsNilDependency(t *testing.T) {
	if _, err := WriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestCommentReviewRequiresReviewerIdentity(t *testing.T) {
	stub := &stubReview{res: &commentdto.ReviewResp{Affected: 2}}
	if _, err := callReview(t, stub, 0, baseReviewArgs()); err == nil {
		t.Fatal("没有操作人身份时必须拒绝")
	}
	if stub.got != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

func TestCommentReviewPassesReviewerFromContext(t *testing.T) {
	stub := &stubReview{res: &commentdto.ReviewResp{Affected: 2, Status: "approved"}}
	if _, err := callReview(t, stub, 42, baseReviewArgs()); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.got.ReviewerID != 42 {
		t.Errorf("审核人应取自 context，实得 %d", stub.got.ReviewerID)
	}
}

func TestCommentReviewSchemaHasNoReviewerField(t *testing.T) {
	tool := mustReviewTool(t, &stubReview{})
	raw, err := tool.SchemaJSON()
	if err != nil {
		t.Fatalf("取 schema 失败: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("解析 schema 失败: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	for _, forbidden := range []string{"reviewerId", "reviewerID", "reviewer"} {
		if _, ok := props[forbidden]; ok {
			t.Errorf("参数里不该有 %q —— 操作人只能取自登录身份", forbidden)
		}
	}
}

func TestCommentReviewRejectsEmptyIDs(t *testing.T) {
	stub := &stubReview{}
	if _, err := callReview(t, stub, 42, map[string]any{
		"projectId": "p-1", "ids": []int64{}, "status": "approved",
	}); err == nil {
		t.Fatal("ids 为空应报错")
	}
	if stub.got != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

func TestCommentReviewRejectsTooManyIDs(t *testing.T) {
	ids := make([]int64, 0, 201)
	for i := 1; i <= 201; i++ {
		ids = append(ids, int64(i))
	}
	stub := &stubReview{}
	if _, err := callReview(t, stub, 42, map[string]any{
		"projectId": "p-1", "ids": ids, "status": "approved",
	}); err == nil {
		t.Fatal("超过 200 条应报错")
	}
}

func TestCommentReviewRejectsBadStatus(t *testing.T) {

	stub := &stubReview{}
	if _, err := callReview(t, stub, 42, map[string]any{
		"projectId": "p-1", "ids": []int64{1}, "status": "pending",
	}); err == nil {
		t.Fatal("pending 不该被接受")
	}
}

func TestCommentReviewDedupesIDs(t *testing.T) {
	stub := &stubReview{res: &commentdto.ReviewResp{Affected: 2, Status: "approved"}}
	if _, err := callReview(t, stub, 42, map[string]any{
		"projectId": "p-1", "ids": []int64{5, 5, 6, 5}, "status": "approved",
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if len(stub.got.IDs) != 2 || stub.got.IDs[0] != 5 || stub.got.IDs[1] != 6 {
		t.Errorf("应按原顺序去重，实得 %v", stub.got.IDs)
	}
}

func TestCommentReviewTextExplainsShortfall(t *testing.T) {
	stub := &stubReview{res: &commentdto.ReviewResp{Affected: 1, Status: "approved"}}
	text, err := callReview(t, stub, 42, baseReviewArgs())
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"已通过", "请求了 2 条", "实际改动 1 条"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

func TestCommentReviewTextDistinguishesReject(t *testing.T) {
	stub := &stubReview{res: &commentdto.ReviewResp{Affected: 2, Status: "rejected"}}
	text, err := callReview(t, stub, 42, map[string]any{
		"projectId": "p-1", "ids": []int64{1, 2}, "status": "rejected",
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !strings.Contains(text, "已驳回") || !strings.Contains(text, "记录仍然保留") {
		t.Errorf("驳回的说明不对：\n%s", text)
	}
}

func TestCommentReviewPropagatesServiceError(t *testing.T) {
	stub := &stubReview{err: errors.New("工程不存在")}
	if _, err := callReview(t, stub, 42, baseReviewArgs()); err == nil {
		t.Fatal("service 报错必须传出去")
	}
}
