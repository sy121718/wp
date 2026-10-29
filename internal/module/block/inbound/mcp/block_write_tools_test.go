package blockmcp

// block_write_tools_test.go — 全局块工具的边界。
//
// 这一批的判据集中在两处**看不见的后果**上：
//   - reuseMode 的 global / template 差别（光看名字分不出来）；
//   - 强制删除被引用的块会让页面少一截（默认是拒绝的）。
// 以及一条工程约束：工具不碰块内 AST（更新时把读回来的 document 原样带回去）。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
)

type stubBlockStore struct{}

func (stubBlockStore) Lookup(context.Context, string, string) (mcp.Result, bool, error) {
	return mcp.Result{}, false, nil
}
func (stubBlockStore) Save(context.Context, string, string, mcp.Result) error { return nil }

type stubBlockRW struct {
	created *blockdto.CreateReq
	updated *blockdto.UpdateReq
	deleted *blockdto.DeleteReq
	listRes []blockdto.BlockResp
	detail  *blockdto.BlockResp
}

func (s *stubBlockRW) List(context.Context, *blockdto.ListReq) ([]blockdto.BlockResp, error) {
	return s.listRes, nil
}
func (s *stubBlockRW) Detail(context.Context, *blockdto.DetailReq) (*blockdto.BlockResp, error) {
	return s.detail, nil
}
func (s *stubBlockRW) Create(_ context.Context, req *blockdto.CreateReq) (*blockdto.BlockResp, error) {
	s.created = req
	return s.detail, nil
}
func (s *stubBlockRW) Update(_ context.Context, req *blockdto.UpdateReq) (*blockdto.BlockResp, error) {
	s.updated = req
	return s.detail, nil
}
func (s *stubBlockRW) Delete(_ context.Context, req *blockdto.DeleteReq) error {
	s.deleted = req
	return nil
}

func blockTool(t *testing.T, name string, s *stubBlockRW, write bool) mcp.Tool {
	t.Helper()
	var (
		tools []mcp.Tool
		err   error
	)
	if write {
		tools, err = WriteTools(s, s, stubBlockStore{})
	} else {
		tools, err = QueryTools(s)
	}
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

func invokeBlock(t *testing.T, tool mcp.Tool, args map[string]any, write bool) (string, error) {
	t.Helper()
	full := map[string]any{}
	for k, v := range args {
		full[k] = v
	}
	if write {
		full["confirm"] = true
		full["idempotencyKey"] = "k-" + t.Name()
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

func TestBlockRejectsNilDeps(t *testing.T) {
	if _, err := QueryTools(nil); err == nil {
		t.Fatal("读器 nil 应报错")
	}
	if _, err := WriteTools(nil, &stubBlockRW{}, stubBlockStore{}); err == nil {
		t.Fatal("写器 nil 应报错")
	}
	if _, err := WriteTools(&stubBlockRW{}, nil, stubBlockStore{}); err == nil {
		t.Fatal("读器 nil 应报错")
	}
	if _, err := WriteTools(&stubBlockRW{}, &stubBlockRW{}, nil); err == nil {
		t.Fatal("幂等存储 nil 应报错")
	}
}

var _ blockcontract.BlockReader = (*stubBlockRW)(nil)
var _ blockcontract.BlockWriter = (*stubBlockRW)(nil)

func TestBlockToolNames(t *testing.T) {
	reads, _ := QueryTools(&stubBlockRW{})
	writes, _ := WriteTools(&stubBlockRW{}, &stubBlockRW{}, stubBlockStore{})
	if len(reads) != 2 || len(writes) != 3 {
		t.Fatalf("应有 2 读 + 3 写，实得 %d + %d", len(reads), len(writes))
	}
}

// global / template 的差别必须写在描述里 —— 两个词光看名字分不出来。
func TestBlockReuseModeDescribed(t *testing.T) {
	create := blockTool(t, "block_create", &stubBlockRW{}, true)
	desc := create.Description()
	if !strings.Contains(desc, "global") || !strings.Contains(desc, "template") {
		t.Errorf("建块描述应解释两种复用模式：\n%s", desc)
	}
	list := blockTool(t, "block_list", &stubBlockRW{}, false)
	if !strings.Contains(list.Description(), "global") {
		t.Errorf("列表描述应提醒确认复用模式：\n%s", list.Description())
	}
}

func TestBlockReuseModeTextCarriesConsequence(t *testing.T) {
	if got := reuseModeText("global"); !strings.Contains(got, "一起变") {
		t.Errorf("global 应说明改动会传播，实得 %q", got)
	}
	if got := reuseModeText("template"); !strings.Contains(got, "复制") {
		t.Errorf("template 应说明是复制来源，实得 %q", got)
	}
}

// 建块给的是空文档，且形状必须与 builder.Page 对齐。
func TestBlockCreateSendsEmptyDocument(t *testing.T) {
	s := &stubBlockRW{detail: &blockdto.BlockResp{ID: "b1", Name: "站点页脚", ReuseMode: "global"}}
	text, err := invokeBlock(t, blockTool(t, "block_create", s, true), map[string]any{
		"projectId": "pr1", "name": "站点页脚",
	}, true)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	doc := strings.TrimSpace(string(s.created.Document))
	if !strings.Contains(doc, `"settings"`) || !strings.Contains(doc, `"root":[]`) {
		t.Errorf("空文档形状应与 builder.Page 对齐，实得 %q", doc)
	}
	if !strings.Contains(text, "编辑器") {
		t.Errorf("回执应指向编辑器去放内容：%s", text)
	}
	// global 的回执要让模型知道这处改动会影响全站。
	if !strings.Contains(text, "所有引用") && !strings.Contains(text, "一起变") {
		t.Errorf("global 的回执应说明影响面：%s", text)
	}
}

func TestBlockCreateRequiresName(t *testing.T) {
	s := &stubBlockRW{}
	if _, err := invokeBlock(t, blockTool(t, "block_create", s, true), map[string]any{"projectId": "pr1", "name": "  "}, true); err == nil {
		t.Fatal("空名称应报错")
	}
	if s.created != nil {
		t.Error("参数不合法时不该调用 service")
	}
}

// 更新是整树覆盖：调用方不说 name 时，工具要把读回来的原值合并回去，
// 而不是把 kind / document 抹掉。
func TestBlockUpdateMergesCurrentValues(t *testing.T) {
	s := &stubBlockRW{detail: &blockdto.BlockResp{
		ID: "b1", Name: "旧名", Kind: "footer", Category: "general", Version: 7,
		ReuseMode: "global", Document: json.RawMessage(`{"settings":{},"root":[{"x":1}]}`),
	}}
	if _, err := invokeBlock(t, blockTool(t, "block_update", s, true), map[string]any{
		"projectId": "pr1", "blockId": "b1", "category": "legal", "expectedVersion": 3,
	}, true); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.updated.ExpectedVersion != 3 {
		t.Fatalf("必须传调用方版本3，不能偷用最新版本7: %+v", s.updated)
	}
	if s.updated.Name != "旧名" {
		t.Errorf("name 应合并回原值，实得 %q", s.updated.Name)
	}
	if s.updated.Kind != "footer" {
		t.Errorf("kind 应保留原值，实得 %q", s.updated.Kind)
	}
	if !strings.Contains(string(s.updated.Document), `"x":1`) {
		t.Errorf("document 必须原样带回（工具不碰块结构），实得 %s", s.updated.Document)
	}
	if s.updated.Category != "legal" {
		t.Errorf("category 应更新，实得 %q", s.updated.Category)
	}
}

// 工具不再承诺绕过引用，旧输入也只能走相同的受保护删除请求。
func TestBlockDeleteHasNoForceBypass(t *testing.T) {
	s := &stubBlockRW{detail: &blockdto.BlockResp{ID: "b1", Name: "公告条"}}
	tool := blockTool(t, "block_delete", s, true)
	schema, err := tool.SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(schema), `"force"`) || strings.Contains(tool.Description(), "强行删除") {
		t.Fatal("工具不得暴露强制删除")
	}
	text, err := invokeBlock(t, tool, map[string]any{"projectId": "pr1", "blockId": "b1", "force": true}, true)
	if err != nil {
		return
	} // 严格schema拒绝旧参数也符合合同。
	if s.deleted == nil || s.deleted.ID != "b1" || strings.Contains(text, "强制") {
		t.Fatalf("旧参数不得改变受保护请求: %+v %s", s.deleted, text)
	}
}

func TestBlockDeleteRequiresID(t *testing.T) {
	s := &stubBlockRW{}
	if _, err := invokeBlock(t, blockTool(t, "block_delete", s, true), map[string]any{"projectId": "pr1", "blockId": " "}, true); err == nil {
		t.Fatal("空 id 应报错")
	}
	if s.deleted != nil {
		t.Error("参数不合法时不该调用 service")
	}
}

func TestBlockUpdateRejectsMissingVersion(t *testing.T) {
	s := &stubBlockRW{}
	_, err := invokeBlock(t, blockTool(t, "block_update", s, true), map[string]any{"projectId": "pr1", "blockId": "b1"}, true)
	if err == nil || s.updated != nil {
		t.Fatalf("缺读取版本必须在调用服务前拒绝: err=%v updated=%+v", err, s.updated)
	}
}
