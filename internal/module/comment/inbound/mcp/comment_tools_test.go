package commentmcp

// comment_tools_test.go — 评论工具的元信息、入参映射与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text（Data 只给渲染层），
// 正文里少了哪个字，用户就看不到哪个字。这里钉的是几件会直接决定回答对错的事：
//   · 评论 id 必须逐条出现 —— 它是运营在后台找到这条评论的唯一钥匙；
//   · 状态文案走 commentenums 的真源，不自己写一份中文表（各写一份时新增状态会
//     出现「页面上叫一个名字、AI 嘴里叫另一个」）；
//   · 正文要压成一行并标明还剩多少字 —— 只截不说会让模型以为自己看的是全文；
//   · 空结果要分清「这个筛选下没有」与「页码超了」，两者对用户的意义不同。
//
// 注意测试传参用 map 而不是参数结构体：结构体序列化会把零值写成 ""，
// 而 mcp 的 Enum 白名单**拒绝空串**（validate.go 的 validateValue）——
// 模型不传某个可选参数时 JSON 里根本没有那个键，两者不是一回事。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/pkg/utils"
)

// stubQuery 假 reader：记录入参、回放固定结果。
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

// 不传 status / entityType 时不能透传空串把筛选变成「只看某个空状态」——
// AdminList 的语义是空串 = 不过滤，这里钉住工具确实原样传了空串（不是别的值）。
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
		{0, commentFindDefaultPageSize},  // 不传（JSON 里没这个键）
		{-3, commentFindDefaultPageSize}, // 负数不是「不要」
		{200, commentFindMaxPageSize},    // 超上限要压下来，否则一次能拉全库
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
	// 状态用 enums 的中文兜底（StatusLabel 为空 —— 工具调用没有请求语言）
	if !strings.Contains(text, commentenums.LabelStatusPending) {
		t.Errorf("状态应当回落成 %q：\n%s", commentenums.LabelStatusPending, text)
	}
	if !strings.Contains(text, "（回复）") {
		t.Errorf("回复标记缺失：\n%s", text)
	}
}

// StatusLabel 有值时优先用它（服务端按请求语言取好的成品），不覆盖成中文兜底。
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

// 空结果的两种说法不能混：总数 0 = 这个筛选下确实没有；总数 >0 而本页 0 条 = 页码超了。
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

// 工具描述里必须说清「本工具只读、改状态用 comment_review」：
// 模型据描述决定说什么，缺了它会有「我帮你把这批评论通过了」这种回答 ——
// 而 comment_find 根本没有这个能力（能力在另一个工具上，且要用户先确认）。
func TestCommentFindDescriptionStatesReadOnly(t *testing.T) {
	tool := mustQueryTools(t, &stubQuery{})["comment_find"]
	desc := tool.Description()
	for _, want := range []string{"只读", "comment_review"} {
		if !strings.Contains(desc, want) {
			t.Errorf("描述里缺 %q：\n%s", want, desc)
		}
	}
}
