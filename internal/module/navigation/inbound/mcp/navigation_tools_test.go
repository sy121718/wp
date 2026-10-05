package navigationmcp

// navigation_tools_test.go — 导航工具的边界。
//
// 两处会静默出错的地方：① kind 传错时 service 默认兜底成 header ——
// 「以为加在页脚、结果长在页眉上」看起来完全正常；② 删一项会连子菜单一起删，
// 而层级只在树里以缩进体现，看错一行就少一整块菜单。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	navigationdto "go_wp/internal/module/navigation/dto"
)

type stubNav struct {
	tree     []*navigationdto.NavigationNode
	treeKind string

	createReq *navigationdto.CreateReq
	createRes *navigationdto.NavigationResp
	updateReq *navigationdto.UpdateReq
	updateRes *navigationdto.NavigationResp
	deleteReq *navigationdto.DeleteReq
}

func (s *stubNav) List(_ context.Context, _ *navigationdto.ListReq) ([]*navigationdto.NavigationResp, error) {
	return nil, nil
}

func (s *stubNav) Tree(_ context.Context, _, kind string) ([]*navigationdto.NavigationNode, error) {
	s.treeKind = kind
	return s.tree, nil
}

func (s *stubNav) Create(_ context.Context, req *navigationdto.CreateReq) (*navigationdto.NavigationResp, error) {
	s.createReq = req
	return s.createRes, nil
}

func (s *stubNav) Update(_ context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	s.updateReq = req
	return s.updateRes, nil
}

func (s *stubNav) Delete(_ context.Context, req *navigationdto.DeleteReq) error {
	s.deleteReq = req
	return nil
}

func navTool(t *testing.T, name string, s *stubNav) mcp.Tool {
	t.Helper()
	tools, err := Tools(s, s)
	if err != nil {
		t.Fatalf("装配导航工具失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

func invokeNavWrite(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
	t.Helper()
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(mcp.WithUserID(context.Background(), 9), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func TestNavigationRejectsNilDeps(t *testing.T) {
	if _, err := Tools(nil, &stubNav{}); err == nil {
		t.Fatal("reader 为 nil 应报错")
	}
	if _, err := Tools(&stubNav{}, nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestNavigationToolNames(t *testing.T) {
	tools, err := Tools(&stubNav{}, &stubNav{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("应有 4 个工具，实得 %d", len(tools))
	}
}

// kind 不认识时当场报错，不能让 service 静默兜底成 header。
func TestNavigationRejectsUnknownKind(t *testing.T) {
	s := &stubNav{}
	_, err := invokeNavWrite(t, navTool(t, "navigation_create", s), map[string]any{
		"projectId": "p1", "title": "关于我们", "path": "/about", "kind": "sidebar",
	})
	if err == nil {
		t.Fatal("不认识的 kind 应报错（service 会静默兜底成 header）")
	}
	if s.createReq != nil {
		t.Error("参数不合法时不应该调用 service")
	}
}

func TestNavigationTreeIndentsHierarchy(t *testing.T) {
	s := &stubNav{tree: []*navigationdto.NavigationNode{
		{ID: "n1", Title: "产品", Path: "/products", Children: []*navigationdto.NavigationNode{
			{ID: "n2", Title: "电子烟", Path: "/products/vape"},
		}},
		{ID: "n3", Title: "关于", Path: "/about"},
	}}
	raw, _ := json.Marshal(map[string]any{"projectId": "p1", "kind": "header"})
	res, err := navTool(t, "navigation_tree", s).Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.treeKind != "header" {
		t.Errorf("kind 没传到: %q", s.treeKind)
	}
	for _, want := range []string{"id=n1", "id=n2", "id=n3", "3 项", "/products/vape"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("树应含 %q，实得：%s", want, res.Text)
		}
	}
	// 子项必须有缩进 —— 位置关系是这棵树唯一重要的信息。
	if !strings.Contains(res.Text, "-   id=n2") {
		t.Errorf("子项应有缩进，实得：%s", res.Text)
	}
}

func TestNavigationTreeGuidesWhenEmpty(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"projectId": "p1", "kind": "footer"})
	res, err := navTool(t, "navigation_tree", &stubNav{}).Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(res.Text, "navigation_create") {
		t.Errorf("空树应指向 navigation_create，实得：%s", res.Text)
	}
	if !strings.Contains(res.Text, "页脚") {
		t.Errorf("空树应说明是哪个位置，实得：%s", res.Text)
	}
}

// 来源类型选了非 custom 就得给 sourceId，否则服务端拒 —— 回执与描述都要说清。
func TestNavigationCreatePassesSource(t *testing.T) {
	s := &stubNav{createRes: &navigationdto.NavigationResp{
		ID: "n1", Title: "关于我们", Path: "/about", Kind: "footer",
	}}
	text, err := invokeNavWrite(t, navTool(t, "navigation_create", s), map[string]any{
		"projectId": "p1", "title": "关于我们", "path": "/about", "kind": "footer",
		"sourceType": "page", "sourceId": "pg1", "target": "blank",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.createReq.SourceID == nil || *s.createReq.SourceID != "pg1" {
		t.Fatalf("sourceId 没传到: %+v", s.createReq.SourceID)
	}
	if !strings.Contains(text, "页脚") {
		t.Errorf("回执应报出位置，实得：%s", text)
	}
	if !strings.Contains(text, "顶层项") {
		t.Errorf("没给 parentId 时应说明是顶层项，实得：%s", text)
	}
}

func TestNavigationCreateTextMarksChild(t *testing.T) {
	pid := "n1"
	s := &stubNav{createRes: &navigationdto.NavigationResp{
		ID: "n2", Title: "电子烟", Path: "/vape", Kind: "header", ParentID: &pid,
	}}
	text, err := invokeNavWrite(t, navTool(t, "navigation_create", s), map[string]any{
		"projectId": "p1", "title": "电子烟", "path": "/vape", "kind": "header", "parentId": "n1",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "n1") {
		t.Errorf("回执应报出父项 id（挂错父项是这步最大的风险），实得：%s", text)
	}
}

// parentId 三态：不传 = 不改层级；空串 = 提升为顶层；id = 挂靠。
func TestNavigationUpdateParentIDIsTriState(t *testing.T) {
	stub := &stubNav{updateRes: &navigationdto.NavigationResp{ID: "n1", Title: "x", Kind: "header"}}
	if _, err := invokeNavWrite(t, navTool(t, "navigation_update", stub), map[string]any{
		"id": "n1", "title": "x",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.updateReq.ParentID != nil {
		t.Errorf("不传 parentId 应为 nil（不改层级），实得 %v", *stub.updateReq.ParentID)
	}

	stub2 := &stubNav{updateRes: &navigationdto.NavigationResp{ID: "n1", Title: "x", Kind: "header"}}
	if _, err := invokeNavWrite(t, navTool(t, "navigation_update", stub2), map[string]any{
		"id": "n1", "parentId": "",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub2.updateReq.ParentID == nil {
		t.Error("传空串应提升为顶层（指针指向空串），实得 nil")
	}
}

func TestNavigationUpdateRejectsEmptyPatch(t *testing.T) {
	s := &stubNav{}
	if _, err := invokeNavWrite(t, navTool(t, "navigation_update", s), map[string]any{"id": "n1"}); err == nil {
		t.Fatal("没有任何要改的字段时应报错")
	}
	if s.updateReq != nil {
		t.Error("参数不合法时不应该调用 service")
	}
}

func TestNavigationUpdateRejectsUnknownKind(t *testing.T) {
	s := &stubNav{}
	if _, err := invokeNavWrite(t, navTool(t, "navigation_update", s), map[string]any{
		"id": "n1", "kind": "sidebar",
	}); err == nil {
		t.Fatal("不认识的 kind 应报错")
	}
}

// 删除会连子菜单一起删 —— 回执必须说出来。
func TestNavigationDeleteTextWarnsAboutChildren(t *testing.T) {
	s := &stubNav{}
	text, err := invokeNavWrite(t, navTool(t, "navigation_delete", s), map[string]any{"id": "n1"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.deleteReq == nil || s.deleteReq.ID != "n1" {
		t.Fatalf("id 没传到: %+v", s.deleteReq)
	}
	if !strings.Contains(text, "子菜单") {
		t.Errorf("删除回执应提醒子树会一起消失，实得：%s", text)
	}
}

// 「先读树」必须写进写工具的描述 —— 否则模型会瞎猜 parentId。
func TestNavigationWriteDescriptionsRequireTreeFirst(t *testing.T) {
	for _, name := range []string{"navigation_create"} {
		desc := navTool(t, name, &stubNav{}).Description()
		if !strings.Contains(desc, "navigation_tree") {
			t.Errorf("%s 的描述应要求先读树：\n%s", name, desc)
		}
	}
	if desc := navTool(t, "navigation_update", &stubNav{}).Description(); !strings.Contains(desc, "三态") {
		t.Errorf("update 的描述要点明 parentId 是三态：\n%s", desc)
	}
}
