package pagemcp

// page_write_tools_test.go — 页面写工具的边界。
//
// 这一批最重要的一条约束是**它不碰页面文档**：建页必须给空文档，
// 页面的结构只能由可视化编辑器写。测试在这里钉住。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	pagedto "go_wp/internal/module/page/dto"
)

type stubPageWriter struct {
	createReq  *pagedto.CreateReq
	createRes  *pagedto.PageResp
	publishReq *pagedto.PublishReq
	publishRes *pagedto.PublishResp
	urlReq     *pagedto.UpdateURLReq
	urlRes     *pagedto.PublishResp
	deleteReq  *pagedto.DeleteReq
}

func (s *stubPageWriter) Create(_ context.Context, req *pagedto.CreateReq) (*pagedto.PageResp, error) {
	s.createReq = req
	return s.createRes, nil
}

func (s *stubPageWriter) Publish(_ context.Context, req *pagedto.PublishReq) (*pagedto.PublishResp, error) {
	s.publishReq = req
	return s.publishRes, nil
}

func (s *stubPageWriter) UpdateURL(_ context.Context, req *pagedto.UpdateURLReq) (*pagedto.PublishResp, error) {
	s.urlReq = req
	return s.urlRes, nil
}

func (s *stubPageWriter) Delete(_ context.Context, req *pagedto.DeleteReq) error {
	s.deleteReq = req
	return nil
}

func pageWriteTool(t *testing.T, name string, s *stubPageWriter) mcp.Tool {
	t.Helper()
	tools, err := WriteTools(s)
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

func invokePageWrite(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
	t.Helper()
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(mcp.WithUserID(context.Background(), 7), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func TestPageWriteRejectsNilWriter(t *testing.T) {
	if _, err := WriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestPageWriteToolNames(t *testing.T) {
	tools, err := WriteTools(&stubPageWriter{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("应有 4 个工具，实得 %d", len(tools))
	}
	for _, want := range []string{"page_create", "page_publish", "page_url_update", "page_delete"} {
		found := false
		for _, tool := range tools {
			if tool.Name() == want {
				found = true
			}
		}
		if !found {
			t.Errorf("缺工具 %s", want)
		}
	}
}

// 建页必须给空文档 —— 页面结构只能由可视化编辑器写。
func TestPageCreateSendsEmptyDocument(t *testing.T) {
	s := &stubPageWriter{createRes: &pagedto.PageResp{ID: "p1", DraftPath: "/about"}}
	text, err := invokePageWrite(t, pageWriteTool(t, "page_create", s), map[string]any{
		"projectId": "pr1", "kind": "home", "draftPath": "/about",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	doc := strings.TrimSpace(string(s.createReq.DraftDocument))
	if doc == "" || doc == "{}" {
		t.Fatalf("建页应给一个能被编辑器打开的空文档，实得 %q", doc)
	}
	// 形状必须与 builder.Page 对齐：settings 是对象、root 是数组。
	if !strings.Contains(doc, `"settings"`) || !strings.Contains(doc, `"root":[]`) {
		t.Errorf("空文档形状应与 builder.Page 对齐（settings 对象 + root 数组），实得 %q", doc)
	}
	if s.createReq.ContentTargetType != "none" {
		t.Errorf("功能页必须声明 contentTargetType=none（pages 表有 content contract 约束），实得 %q", s.createReq.ContentTargetType)
	}
	if !strings.Contains(text, "可视化编辑器") {
		t.Errorf("回执应指向编辑器去放内容，实得：%s", text)
	}
}

func TestPageCreateNormalizesPath(t *testing.T) {
	s := &stubPageWriter{}
	if _, err := invokePageWrite(t, pageWriteTool(t, "page_create", s), map[string]any{
		"projectId": "pr1", "kind": "home", "draftPath": "about",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.createReq.DraftPath != "/about" {
		t.Errorf("路径应补前导斜杠，实得 %q", s.createReq.DraftPath)
	}
}

// 发布回执要给出线上地址，用户下一步就是拿它去访问。
func TestPagePublishReportsPath(t *testing.T) {
	s := &stubPageWriter{publishRes: &pagedto.PublishResp{
		PageID: "p1", Status: "active", DraftPath: "/about", ActiveHash: "abcdef1234567890",
	}}
	text, err := invokePageWrite(t, pageWriteTool(t, "page_publish", s), map[string]any{"pageId": "p1"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "/about") {
		t.Errorf("发布回执应报出路径，实得：%s", text)
	}
	if !strings.Contains(text, "abcdef12") {
		t.Errorf("发布回执应报出线上产物（截短），实得：%s", text)
	}
	if strings.Contains(text, "abcdef1234567890") {
		t.Errorf("哈希应截短，实得：%s", text)
	}
}

// 改网址不加跳转时必须说出来 —— 老地址会 404。
func TestPageURLUpdateWarnsWhenNoRedirect(t *testing.T) {
	s := &stubPageWriter{urlRes: &pagedto.PublishResp{PageID: "p1", Status: "active", DraftPath: "/about-us"}}
	text, err := invokePageWrite(t, pageWriteTool(t, "page_url_update", s), map[string]any{
		"pageId": "p1", "newPath": "/about-us",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "404") {
		t.Errorf("没加跳转时应提醒老地址 404，实得：%s", text)
	}

	s2 := &stubPageWriter{urlRes: &pagedto.PublishResp{PageID: "p1", Status: "active", DraftPath: "/about-us"}}
	text2, err := invokePageWrite(t, pageWriteTool(t, "page_url_update", s2), map[string]any{
		"pageId": "p1", "newPath": "/about-us", "withRedirect": true,
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if strings.Contains(text2, "404") {
		t.Errorf("加了跳转就不该再提 404，实得：%s", text2)
	}
}

// 删除是破坏性的 —— 描述必须提醒先确认，并点名功能页不该删。
func TestPageDeleteDescriptionWarns(t *testing.T) {
	desc := pageWriteTool(t, "page_delete", &stubPageWriter{}).Description()
	if !strings.Contains(desc, "page_find") {
		t.Errorf("删除描述应要求先确认目标：\n%s", desc)
	}
	if !strings.Contains(desc, "不可撤销") {
		t.Errorf("删除描述应点明不可撤销：\n%s", desc)
	}
	if !strings.Contains(desc, "404") {
		t.Errorf("删除描述应提醒功能页不该删：\n%s", desc)
	}
}

func TestPageDeleteRejectsEmptyID(t *testing.T) {
	s := &stubPageWriter{}
	if _, err := invokePageWrite(t, pageWriteTool(t, "page_delete", s), map[string]any{"pageId": "  "}); err == nil {
		t.Fatal("空 id 应报错")
	}
	if s.deleteReq != nil {
		t.Error("参数不合法时不该调用 service")
	}
}

// 发布必须让人确认 —— 它会让改动立刻对访客可见。
func TestPagePublishDescriptionRequiresConfirmTone(t *testing.T) {
	desc := pageWriteTool(t, "page_publish", &stubPageWriter{}).Description()
	if !strings.Contains(desc, "确认") {
		t.Errorf("发布描述应要求先跟用户确认：\n%s", desc)
	}
}

// 只能建功能页：普通内容页必须绑内容（pages 表 constraint），
// 而内容页是跟着内容自动带出来的，不该在这里凭空造。
func TestPageCreateKindEnumOnlyStructural(t *testing.T) {
	tool := pageWriteTool(t, "page_create", &stubPageWriter{})
	schema := strings.TrimSpace(string(mustJSON(t, tool)))
	for _, kind := range []string{"home", "archive", "search", "notFound"} {
		if !strings.Contains(schema, kind) {
			t.Errorf("kind 枚举应含 %q：\n%s", kind, schema)
		}
	}
	for _, bad := range []string{`"page"`, `"article"`, `"tag"`} {
		if strings.Contains(schema, bad) {
			t.Errorf("kind 枚举不该含 %s（它们必须绑内容，页面跟着内容走）：\n%s", bad, schema)
		}
	}
	if desc := tool.Description(); !strings.Contains(desc, "page_find") {
		t.Errorf("建功能页的描述应先让模型查有没有同用途的页面：\n%s", desc)
	}
}

func mustJSON(t *testing.T, tool mcp.Tool) []byte {
	t.Helper()
	raw, err := tool.SchemaJSON()
	if err != nil {
		t.Fatalf("取 schema 失败: %v", err)
	}
	return raw
}
