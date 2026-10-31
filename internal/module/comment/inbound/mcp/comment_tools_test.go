package commentmcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/pkg/utils"
)

type stubQuery struct {
	gotList *commentdto.AdminListReq
	listRes *commentdto.AdminListResp
	err     error
}

func (s *stubQuery) AdminList(_ context.Context, req *commentdto.AdminListReq) (*commentdto.AdminListResp, error) {
	s.gotList = req
	return s.listRes, s.err
}

func mustQueryTools(t *testing.T, stub *stubQuery) map[string]mcp.Tool {
	t.Helper()
	tools, err := QueryTools(stub)
	if err != nil {
		t.Fatalf("装配工具失败: %v", err)
	}
	out := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = tool
	}
	return out
}

func runCommentFind(t *testing.T, stub *stubQuery, args map[string]any) string {
	t.Helper()
	tool := mustQueryTools(t, stub)["comment_find"]
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	res, err := tool.Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	return res.Text
}

func TestQueryToolsRejectsNilReader(t *testing.T) {
	if _, err := QueryTools(nil); err == nil {
		t.Fatal("依赖缺失时必须报错，而不是回一个没有依赖的工具集")
	}
}

func TestCommentFindMapsStatusAndKeyword(t *testing.T) {
	stub := &stubQuery{listRes: &commentdto.AdminListResp{}}
	runCommentFind(t, stub, map[string]any{
		"projectId": "p-1",
		"status":    commentenums.StatusPending,
		"keyword":   "退货",
		"pageSize":  20,
	})
	if stub.gotList == nil {
		t.Fatal("没有透传到 reader")
	}
	if stub.gotList.Status != commentenums.StatusPending {
		t.Errorf("status = %q，期望 %q", stub.gotList.Status, commentenums.StatusPending)
	}
	if stub.gotList.Keyword != "退货" {
		t.Errorf("keyword = %q", stub.gotList.Keyword)
	}
	if stub.gotList.PageSize != 20 {
		t.Errorf("pageSize = %d，期望 20", stub.gotList.PageSize)
	}
}

func TestCommentFindKeepsEmptyFiltersUnset(t *testing.T) {
	stub := &stubQuery{listRes: &commentdto.AdminListResp{}}
	runCommentFind(t, stub, map[string]any{"projectId": "p-1"})
	if stub.gotList.Status != "" || stub.gotList.EntityType != "" {
		t.Errorf("不传筛选时应当留空，实得 status=%q entityType=%q",
			stub.gotList.Status, stub.gotList.EntityType)
	}
	if stub.gotList.PageSize != commentFindDefaultPageSize {
		t.Errorf("不传 pageSize 应当落到默认值 %d，实得 %d",
			commentFindDefaultPageSize, stub.gotList.PageSize)
	}
}

func TestCommentFindClampsPageSize(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, commentFindDefaultPageSize},
		{-3, commentFindDefaultPageSize},
		{200, commentFindMaxPageSize},
		{7, 7},
	}
	for _, c := range cases {
		stub := &stubQuery{listRes: &commentdto.AdminListResp{}}
		runCommentFind(t, stub, map[string]any{"projectId": "p-1", "pageSize": c.in})
		if stub.gotList.PageSize != c.want {
			t.Errorf("pageSize %d → %d，期望 %d", c.in, stub.gotList.PageSize, c.want)
		}
	}
}

func TestCommentFindTextListsIDStatusAndBody(t *testing.T) {
	stub := &stubQuery{listRes: &commentdto.AdminListResp{
		Total: 2,
		Items: []commentdto.AdminItem{
			{ID: 41, Body: "这个尺寸偏小", Status: commentenums.StatusPending,
				EntityType: "product", EntityID: "p-9", UserID: 12,
				CreateTime: utils.JSONTime{}, IsReply: false},
			{ID: 42, Body: "谢谢反馈", Status: commentenums.StatusApproved,
				EntityType: "product", EntityID: "p-9", IsReply: true},
		},
	}}
	text := runCommentFind(t, stub, map[string]any{"projectId": "p-1"})
	for _, want := range []string{"#41", "#42", "这个尺寸偏小", "谢谢反馈", "product p-9"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}

	if !strings.Contains(text, commentenums.LabelStatusPending) {
		t.Errorf("状态应当回落成 %q：\n%s", commentenums.LabelStatusPending, text)
	}
	if !strings.Contains(text, "（回复）") {
		t.Errorf("回复标记缺失：\n%s", text)
	}
}

func TestCommentFindPrefersServerStatusLabel(t *testing.T) {
	stub := &stubQuery{listRes: &commentdto.AdminListResp{
		Total: 1,
		Items: []commentdto.AdminItem{
			{ID: 1, Body: "x", Status: commentenums.StatusPending, StatusLabel: "Pending review"},
		},
	}}
	text := runCommentFind(t, stub, map[string]any{"projectId": "p-1"})
	if !strings.Contains(text, "Pending review") {
		t.Errorf("应当用服务端给的展示名：\n%s", text)
	}
	if strings.Contains(text, commentenums.LabelStatusPending) {
		t.Errorf("不该同时出现中文兜底：\n%s", text)
	}
}

func TestInlineCommentBodyFlattensAndMarksRemainder(t *testing.T) {
	long := strings.Repeat("字", commentBodyInlineMax+30)
	if got := inlineCommentBody("第一行\n第二行"); got != "第一行 第二行" {
		t.Errorf("换行应当压成空格，实得 %q", got)
	}
	got := inlineCommentBody(long)
	if !strings.Contains(got, "还有 30 字") {
		t.Errorf("截断必须说明还剩多少字，实得 %q", got)
	}
	if len([]rune(got)) < commentBodyInlineMax {
		t.Errorf("截断后不该比上限还短：%d", len([]rune(got)))
	}
	if got := inlineCommentBody("  \n  "); got != "（空正文）" {
		t.Errorf("空正文要有明确说法，实得 %q", got)
	}
}

func TestCommentFindEmptyTextDistinguishesCases(t *testing.T) {
	none := runCommentFind(t, &stubQuery{listRes: &commentdto.AdminListResp{Total: 0}},
		map[string]any{"projectId": "p-1"})
	if !strings.Contains(none, "换个关键词") {
		t.Errorf("总数 0 时应当建议换条件：%s", none)
	}
	beyond := runCommentFind(t, &stubQuery{listRes: &commentdto.AdminListResp{Total: 5}},
		map[string]any{"projectId": "p-1", "page": 9})
	if !strings.Contains(beyond, "页码") {
		t.Errorf("页码超范围时应当指出来：%s", beyond)
	}
}

func TestCommentFindPropagatesReaderError(t *testing.T) {
	stub := &stubQuery{err: errors.New("db down")}
	tool := mustQueryTools(t, stub)["comment_find"]
	raw, _ := json.Marshal(map[string]any{"projectId": "p-1"})
	if _, err := tool.Invoke(context.Background(), raw); err == nil {
		t.Fatal("reader 报错时工具必须把错误传出去，不能吞掉")
	}
}

func TestCommentFindDescriptionStatesReadOnly(t *testing.T) {
	tool := mustQueryTools(t, &stubQuery{})["comment_find"]
	desc := tool.Description()
	for _, want := range []string{"只读", "comment_review"} {
		if !strings.Contains(desc, want) {
			t.Errorf("描述里缺 %q：\n%s", want, desc)
		}
	}
}
