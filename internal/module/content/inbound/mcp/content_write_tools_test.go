package contentmcp

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/mcp"
	contentdto "go_wp/internal/module/content/dto"
)

type fakeContent struct {
	created  []*contentdto.CreateReq
	updated  []*contentdto.UpdateReq
	deleted  []string
	getCalls int
	listReq  *contentdto.ListReq
}

func (f *fakeContent) Create(_ context.Context, req *contentdto.CreateReq) (*contentdto.ContentResp, error) {
	f.created = append(f.created, req)
	return &contentdto.ContentResp{ID: "id-1", EntityType: req.EntityType, Slug: req.Slug, Revision: 1}, nil
}

func (f *fakeContent) Update(_ context.Context, req *contentdto.UpdateReq) (*contentdto.ContentResp, error) {
	f.updated = append(f.updated, req)
	return &contentdto.ContentResp{ID: req.ID, EntityType: "article", Slug: "s", Revision: 2}, nil
}

func (f *fakeContent) Delete(_ context.Context, req *contentdto.DeleteReq) error {
	f.deleted = append(f.deleted, req.ID)
	return nil
}

func (f *fakeContent) List(_ context.Context, req *contentdto.ListReq) ([]*contentdto.ContentResp, error) {
	f.listReq = req
	return []*contentdto.ContentResp{
		{ID: "id-1", EntityType: "article", Slug: "quit-smoking", Data: map[string]any{"title": "如何戒烟"}},
	}, nil
}

func (f *fakeContent) Get(_ context.Context, req *contentdto.GetReq) (*contentdto.ContentResp, error) {
	f.getCalls++
	return &contentdto.ContentResp{
		ID: req.ID, EntityType: "article", Slug: "hello", Revision: 3,
		Data: map[string]any{"title": "标题", "body": "正文", "excerpt": ""},
	}, nil
}

func toolByName(t *testing.T, list []mcp.Tool, name string) mcp.Tool {
	t.Helper()
	for _, tl := range list {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("工具集里没有 %s", name)
	return mcp.Tool{}
}

func TestContentToolsIncludeReaderWithWriters(t *testing.T) {
	f := &fakeContent{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range list {
		names[tl.Name()] = true
	}
	for _, want := range []string{"content_find", "content_get", "content_create", "content_update", "content_delete"} {
		if !names[want] {
			t.Errorf("缺少工具 %s", want)
		}
	}
	if len(list) != 5 {
		t.Errorf("工具条数应为 5，实得 %d", len(list))
	}
}

func TestContentFindTurnsNameIntoID(t *testing.T) {
	f := &fakeContent{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := call2(t, toolByName(t, list, "content_find"), `{"keyword":"戒烟"}`)
	if f.listReq == nil || f.listReq.Keyword != "戒烟" {
		t.Fatalf("关键词没传下去：%+v", f.listReq)
	}
	if !strings.Contains(res.Text, "如何戒烟") || !strings.Contains(res.Text, "id-1") {
		t.Fatalf("结果要同时给出标题与 id（模型下一步要用 id），实得：%s", res.Text)
	}
}

func TestContentGetPointsAtFind(t *testing.T) {
	f := &fakeContent{}
	list, _ := Tools(f, f, nil)
	if !strings.Contains(toolByName(t, list, "content_get").Description(), "content_find") {
		t.Fatal("content_get 的说明里要指出 id 从哪来")
	}
}

func call2(t *testing.T, tool mcp.Tool, body string) mcp.Result {
	t.Helper()
	res, err := tool.Invoke(context.Background(), json.RawMessage(body))
	if err != nil {
		t.Fatalf("%s 调用失败：%v", tool.Name(), err)
	}
	return res
}

func TestContentUpdateMergesOnServer(t *testing.T) {
	f := &fakeContent{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := toolByName(t, list, "content_update")
	if _, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"id":"id-1","data":{"title":"新标题"},"confirm":true,"idempotencyKey":"k1"}`)); err != nil {
		t.Fatal(err)
	}
	if f.getCalls != 1 {
		t.Fatalf("合并前必须先读现值，实得 Get 调用 %d 次", f.getCalls)
	}
	if len(f.updated) != 1 {
		t.Fatalf("应写回一次，实得 %d 次", len(f.updated))
	}
	got := f.updated[0].Data
	if got["title"] != "新标题" {
		t.Errorf("改的字段没写进去：%v", got["title"])
	}

	if got["body"] != "正文" {
		t.Errorf("没提到的字段被丢了（body）：%v", got)
	}
	if _, ok := got["excerpt"]; !ok {
		t.Errorf("没提到的字段被丢了（excerpt）：%v", got)
	}
}

func TestContentUpdateDescriptionSaysIncremental(t *testing.T) {
	f := &fakeContent{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := toolByName(t, list, "content_update")
	if !strings.Contains(tool.Description(), "没写的字段保持原值") {
		t.Fatalf("说明里必须点明增量语义，实得：%s", tool.Description())
	}
	dataDesc := tool.Schema().Properties["data"].Description
	if !strings.Contains(dataDesc, "只传要改的那几个") {
		t.Errorf("data 的字段说明要写清增量语义，实得：%s", dataDesc)
	}
}

func TestContentCreateSchemaUsesContractFields(t *testing.T) {
	f := &fakeContent{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	props := toolByName(t, list, "content_create").Schema().Properties["data"].Properties
	for _, want := range []string{"title", "body", "excerpt"} {
		if _, ok := props[want]; !ok {
			t.Errorf("data 里缺少契约字段 %q", want)
		}
	}

	if _, ok := props["notAField"]; ok {
		t.Error("schema 里出现了白名单外的字段")
	}
}

func TestContentToolsRejectMissingDeps(t *testing.T) {
	f := &fakeContent{}
	if _, err := Tools(nil, f, nil); err == nil {
		t.Error("写服务缺失应报错")
	}
	if _, err := Tools(f, nil, nil); err == nil {
		t.Error("读服务缺失应报错")
	}
}
