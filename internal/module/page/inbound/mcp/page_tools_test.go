package pagemcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	pagedto "go_wp/internal/module/page/dto"
)

type stubReader struct {
	gotProjectID string
	res          []pagedto.PageTitleResp
	err          error
}

func (s *stubReader) ListPageTitles(_ context.Context, projectID string) ([]pagedto.PageTitleResp, error) {
	s.gotProjectID = projectID
	return s.res, s.err
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	return b
}

func mustTool(t *testing.T, stub *stubReader) mcp.Tool {
	t.Helper()
	tools, err := Tools(stub)
	if err != nil {
		t.Fatalf("取页面工具失败: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("页面工具应暴露 1 个，实得 %d", len(tools))
	}
	if tools[0].Name() != "page_find" {
		t.Fatalf("工具名应是 page_find，实得 %q", tools[0].Name())
	}
	return tools[0]
}

func TestToolsRejectsNilReader(t *testing.T) {
	if _, err := Tools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestPageFindRequiresProject(t *testing.T) {
	stub := &stubReader{}
	tool := mustTool(t, stub)
	_, err := tool.Invoke(context.Background(), mustRaw(t, map[string]any{}))
	if err == nil {
		t.Fatal("projectId 必填，缺失应被拦下")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError，实得 %T（%v）", err, err)
	}
	if stub.gotProjectID != "" {
		t.Fatal("参数不合规时不应打到 service")
	}
}

func TestPageFindRanksPrefixBeforeContains(t *testing.T) {
	active := "/help"
	list := []pagedto.PageTitleResp{
		{ID: "p3", SEOTitle: "如何联系帮助台", DraftPath: "/contact-help"},
		{ID: "p2", SEOTitle: "关于帮助中心的历史", DraftPath: "/about-help"},
		{ID: "p1", SEOTitle: "帮助中心", DraftPath: "/help", ActivePath: &active},
	}

	got := rankMatches(list, "帮助")

	want := []string{"p1", "p3", "p2"}
	if len(got) != len(want) {
		t.Fatalf("三条都该命中，实得 %d 条", len(got))
	}
	for i, id := range want {
		if got[i].ID != id {
			ids := make([]string, 0, len(got))
			for _, g := range got {
				ids = append(ids, g.ID+"="+g.SEOTitle)
			}
			t.Fatalf("第 %d 条应是 %s（前缀命中在前，同档保持原始顺序），实得 %v", i+1, id, ids)
		}
	}
	_ = pageListText(list, "帮助")
}

func TestPageFindMatchesPath(t *testing.T) {
	list := []pagedto.PageTitleResp{
		{ID: "p1", SEOTitle: "退款政策", DraftPath: "/refund-policy"},
		{ID: "p2", SEOTitle: "联系我们", DraftPath: "/contact"},
	}
	text := pageListText(list, "refund")
	if !strings.Contains(text, "退款政策") {
		t.Fatalf("路径命中也要算命中：\n%s", text)
	}
	if strings.Contains(text, "联系我们") {
		t.Fatalf("不命中的不该出现在结果里：\n%s", text)
	}
}

func TestPageFindMarksUnpublished(t *testing.T) {
	active := "/live"
	list := []pagedto.PageTitleResp{
		{ID: "p1", SEOTitle: "已发布页", DraftPath: "/draft-a", ActivePath: &active},
		{ID: "p2", SEOTitle: "未发布页", DraftPath: "/draft-b"},
	}
	text := pageListText(list, "")
	if !strings.Contains(text, "线上 /live") {
		t.Fatalf("已发布页要给出线上路径：\n%s", text)
	}
	if !strings.Contains(text, "（未发布）") {
		t.Fatalf("未发布页必须明说（否则模型会拿草稿路径去拼链接）：\n%s", text)
	}
}

func TestPageFindEmptySuggestsAnotherWord(t *testing.T) {
	list := []pagedto.PageTitleResp{{ID: "p1", SEOTitle: "首页", DraftPath: "/"}}
	text := pageListText(list, "退货")
	if !strings.Contains(text, "没有匹配「退货」的页面") {
		t.Fatalf("空结果应回显关键词：%s", text)
	}
	if !strings.Contains(text, "换一个更短的词") {
		t.Fatalf("空结果要给出下一步（换个词 / 不给关键词）：%s", text)
	}
	if !strings.Contains(text, "共 1 个页面") {
		t.Fatalf("空结果也要让模型知道总量（「一个都没有」与「没匹配上」不是一回事）：%s", text)
	}
}

func TestPageFindWithoutKeywordListsAll(t *testing.T) {
	list := []pagedto.PageTitleResp{
		{ID: "p1", SEOTitle: "首页", DraftPath: "/"},
		{ID: "p2", SEOTitle: "关于我们", DraftPath: "/about"},
	}
	text := pageListText(list, "")
	if !strings.Contains(text, "共 2 个页面") {
		t.Fatalf("不给关键词应列出全部：\n%s", text)
	}
	for _, want := range []string{"id=p1", "首页", "id=p2", "关于我们"} {
		if !strings.Contains(text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, text)
		}
	}
}

func TestPageFindTruncatesLongList(t *testing.T) {
	list := make([]pagedto.PageTitleResp, 0, pageMatchLimit+5)
	for i := 0; i < pageMatchLimit+5; i++ {
		list = append(list, pagedto.PageTitleResp{ID: "p", SEOTitle: "页面", DraftPath: "/p"})
	}
	text := pageListText(list, "")
	if !strings.Contains(text, "只列了前") {
		t.Fatalf("超限时要说明只列了一部分（否则模型会以为这就是全部）：\n%s", text)
	}
}
