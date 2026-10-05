package contentmcp

// content_write_tools_test.go — 内容读写工具集的形状断言。
//
// 写工具的行为（确认位 / 幂等）在地基里已由 internal/mcp/write_test.go 覆盖；
// 这里钉的是**内容模块特有的两件事**：读入口必须与写入口同批出现，以及
// update 的说明必须说清「整份替换」—— 这一条写错的后果是模型只传要改的字段，
// 而那次调用会成功返回、看起来完成得很好，只是把别的字段抹了。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	contentdto "go_wp/internal/module/content/dto"
)

// fakeContent 内容读写服务的假实现。
type fakeContent struct {
	created  []*contentdto.CreateReq
	updated  []*contentdto.UpdateReq
	deleted  []string
	getCalls int
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

func (f *fakeContent) Get(_ context.Context, req *contentdto.GetReq) (*contentdto.ContentResp, error) {
	f.getCalls++
	return &contentdto.ContentResp{
		ID: req.ID, EntityType: "article", Slug: "hello", Revision: 3,
		Data: map[string]any{"title": "标题", "body": "正文", "excerpt": ""},
	}, nil
}

// toolByName 按名字取工具。
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

// TestContentToolsIncludeReaderWithWriters 读入口必须与三个写入口同批出现。
//
// 没有 content_get 时，模型要改内容只能凭记忆拼字段集 —— 而 update 是整份替换，
// 漏一个字段就抹一个，且调用成功返回。把这条放进工具集形状的断言里，
// 是为了让「只上写工具」这个改动当场变红。
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
	for _, want := range []string{"content_get", "content_create", "content_update", "content_delete"} {
		if !names[want] {
			t.Errorf("缺少工具 %s", want)
		}
	}
	if len(list) != 4 {
		t.Errorf("工具条数应为 4，实得 %d", len(list))
	}
}

// TestContentUpdateMergesOnServer 合并必须发生在服务端。
//
// 这条是本批最要紧的不变量。原先的设计是「让模型自己取全量再写回」，而真调一次模型
// 才暴露它走不通：content_get 的结果会被剪枝（长正文只留前 N 字），模型拿不到完整字段集，
// 于是它**正确地**拒绝写入 —— 判断没错，但这个死结该由工具解开：模型的意图是
// 「改这个字段」，不是「提交整份文档」。
//
// 断言看的是「Update 收到了什么」：它必须同时含**没提到的原字段**与**被改的字段**。
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
	// f.Get 的返回值里有 body 与 excerpt；它们没出现在入参里，但必须原样写回 ——
	// 少了这一步就等于把模型没看到的字段抹掉，而那次调用会成功返回。
	if got["body"] != "正文" {
		t.Errorf("没提到的字段被丢了（body）：%v", got)
	}
	if _, ok := got["excerpt"]; !ok {
		t.Errorf("没提到的字段被丢了（excerpt）：%v", got)
	}
}

// TestContentUpdateDescriptionSaysIncremental 说明要说清「只传要改的字段」。
//
// 说明写反的后果是模型把整份内容硬凑出来再传（凑不全就抹字段），
// 或者像实测那样直接拒绝写入。
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

// TestContentCreateSchemaUsesContractFields create 的 data 属性来自契约白名单。
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
	// 白名单外的字段不该出现在 schema 里（出现在这里等于告诉模型它可以传）。
	if _, ok := props["notAField"]; ok {
		t.Error("schema 里出现了白名单外的字段")
	}
}

// TestContentToolsRejectMissingDeps 依赖缺失是装配期错误，不是运行期静默降级。
func TestContentToolsRejectMissingDeps(t *testing.T) {
	f := &fakeContent{}
	if _, err := Tools(nil, f, nil); err == nil {
		t.Error("写服务缺失应报错")
	}
	if _, err := Tools(f, nil, nil); err == nil {
		t.Error("读服务缺失应报错")
	}
}
