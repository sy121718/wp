// Package unit 覆盖 reuse_mode 复用方式核心规则（docs/02-D §5/§9/§12）：
// 归一化校验、stale 传播分支、删除引用拦截、CloneAST 独立性。
package unit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
)

// TestBlockReuseModeDefaultsAndValidation 创建链路的 reuse_mode 归一化。
func TestBlockReuseModeDefaultsAndValidation(t *testing.T) {
	e := newEnv(t)

	t.Run("缺省默认 global", func(t *testing.T) {
		b := e.createBlock(t, "页眉A", "header")
		if b.ReuseMode != "global" {
			t.Fatalf("期望默认 global，实际 %q", b.ReuseMode)
		}
	})
	t.Run("template 合法落库", func(t *testing.T) {
		res, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
			ProjectID: e.projectID, Name: "商品卡模板", Kind: "snippet", ReuseMode: "template",
		})
		if err != nil {
			t.Fatalf("创建 template 块失败: %v", err)
		}
		if res.ReuseMode != "template" {
			t.Fatalf("期望 template，实际 %q", res.ReuseMode)
		}
	})
	t.Run("非法 reuseMode 拒绝", func(t *testing.T) {
		_, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
			ProjectID: e.projectID, Name: "坏块", Kind: "block", ReuseMode: "sync",
		})
		if !errors.Is(err, blockcontract.ErrInvalidReuseMode) {
			t.Fatalf("期望 ErrInvalidReuseMode，实际 %v", err)
		}
	})
	t.Run("新 kind 白名单", func(t *testing.T) {
		for _, kind := range []string{"announcement", "sidebar", "cta", "snippet", "grid"} {
			e.createBlock(t, "骨架-"+kind, kind)
		}
		_, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
			ProjectID: e.projectID, Name: "坏类型", Kind: "hero",
		})
		if !errors.Is(err, blockcontract.ErrInvalidKind) {
			t.Fatalf("期望 ErrInvalidKind，实际 %v", err)
		}
	})
}

// TestBlockListFilterReuseMode 列表按 reuseMode 过滤。
func TestBlockListFilterReuseMode(t *testing.T) {
	e := newEnv(t)
	e.createBlock(t, "引用块", "block")
	if _, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: e.projectID, Name: "复制块", Kind: "snippet", ReuseMode: "template",
	}); err != nil {
		t.Fatalf("创建 template 块失败: %v", err)
	}
	for _, tc := range []struct{ mode, name string }{
		{"global", "引用块"}, {"template", "复制块"},
	} {
		list, err := e.svc.List(context.Background(), &blockdto.ListReq{
			ProjectID: e.projectID, ReuseMode: tc.mode,
		})
		if err != nil {
			t.Fatalf("List reuseMode=%s 失败: %v", tc.mode, err)
		}
		if len(list) != 1 || list[0].Name != tc.name {
			t.Fatalf("reuseMode=%s 期望仅 %q，实际 %v", tc.mode, tc.name, list)
		}
	}
}

// TestBlockStalePropagationBranch stale 传播分支（docs/02-D §9）。
func TestBlockStalePropagationBranch(t *testing.T) {
	e := newEnv(t)
	calls := 0
	e.svc.SetStalePropagator(func(context.Context, string) error { calls++; return nil })

	global := e.createBlock(t, "引用块", "block")
	if _, err := e.svc.Update(context.Background(), &blockdto.UpdateReq{ID: global.ID, Name: "引用块2"}); err != nil {
		t.Fatalf("更新 global 块失败: %v", err)
	}
	if calls != 1 {
		t.Fatalf("global 块变更应传播 1 次，实际 %d", calls)
	}

	tpl, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: e.projectID, Name: "复制块", Kind: "snippet", ReuseMode: "template",
	})
	if err != nil {
		t.Fatalf("创建 template 块失败: %v", err)
	}
	if _, err := e.svc.Update(context.Background(), &blockdto.UpdateReq{ID: tpl.ID, Name: "复制块2"}); err != nil {
		t.Fatalf("更新 template 块失败: %v", err)
	}
	if calls != 1 {
		t.Fatalf("template 块变更不应传播，调用数 %d", calls)
	}
}

// TestBlockDeleteReferenceGuard 删除引用拦截（docs/02-D §9）。
func TestBlockDeleteReferenceGuard(t *testing.T) {
	e := newEnv(t)
	e.svc.SetReferenceChecker(func(context.Context, string) (bool, error) { return true, nil })

	global := e.createBlock(t, "被引用块", "header")
	err := e.svc.Delete(context.Background(), &blockdto.DeleteReq{ID: global.ID})
	if !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("期望 ErrBlockInUse，实际 %v", err)
	}
	if err := e.svc.Delete(context.Background(), &blockdto.DeleteReq{ID: global.ID, Force: true}); err != nil {
		t.Fatalf("Force 删除失败: %v", err)
	}

	tpl, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: e.projectID, Name: "复制块", Kind: "snippet", ReuseMode: "template",
	})
	if err != nil {
		t.Fatalf("创建 template 块失败: %v", err)
	}
	// 检查器恒报被引用：template 块仍应直接可删（页面持有副本，删除无副作用）。
	if err := e.svc.Delete(context.Background(), &blockdto.DeleteReq{ID: tpl.ID}); err != nil {
		t.Fatalf("template 块删除不应被拦截: %v", err)
	}
}

// TestBlockUpdateSwitchGlobalToTemplateGuard global→template 切换防御。
func TestBlockUpdateSwitchGlobalToTemplateGuard(t *testing.T) {
	e := newEnv(t)
	e.svc.SetReferenceChecker(func(context.Context, string) (bool, error) { return true, nil })
	b := e.createBlock(t, "引用块", "cta")

	_, err := e.svc.Update(context.Background(), &blockdto.UpdateReq{ID: b.ID, Name: "引用块", ReuseMode: "template"})
	if !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("期望 ErrBlockInUse，实际 %v", err)
	}

	// 无引用时可切换。
	e.svc.SetReferenceChecker(func(context.Context, string) (bool, error) { return false, nil })
	res, err := e.svc.Update(context.Background(), &blockdto.UpdateReq{ID: b.ID, Name: "引用块", ReuseMode: "template"})
	if err != nil || res.ReuseMode != "template" {
		t.Fatalf("无引用切换应成功，res=%+v err=%v", res, err)
	}
}

// collectNodeIDs 递归收集文档 root 树的全部节点 ID。
func collectNodeIDs(doc json.RawMessage) []string {
	var parsed struct {
		Root []map[string]any `json:"root"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return nil
	}
	var ids []string
	var walk func(nodes []map[string]any)
	walk = func(nodes []map[string]any) {
		for _, n := range nodes {
			if id, ok := n["id"].(string); ok {
				ids = append(ids, id)
			}
			if children, ok := n["children"].([]any); ok {
				var subs []map[string]any
				for _, c := range children {
					if m, ok := c.(map[string]any); ok {
						subs = append(subs, m)
					}
				}
				walk(subs)
			}
		}
	}
	walk(parsed.Root)
	return ids
}

// TestBlockCloneASTIndependence CloneAST 独立性（docs/02-D §5.2）：
// 副本节点数量与源一致但全部节点 ID 重生成。
func TestBlockCloneASTIndependence(t *testing.T) {
	e := newEnv(t)
	src := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}},"children":[{"id":"n2","type":"core.text","props":{"text":"b"}}]}]}`
	b, err := e.svc.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: e.projectID, Name: "商品卡", Kind: "snippet", ReuseMode: "template",
		Document: json.RawMessage(src),
	})
	if err != nil {
		t.Fatalf("创建源块失败: %v", err)
	}
	clone, err := e.svc.CloneAST(context.Background(), &blockdto.CloneReq{ID: b.ID})
	if err != nil {
		t.Fatalf("CloneAST 失败: %v", err)
	}

	srcIDs := collectNodeIDs(json.RawMessage(src))
	dstIDs := collectNodeIDs(clone.Document)
	if len(dstIDs) != len(srcIDs) || len(srcIDs) != 2 {
		t.Fatalf("副本节点数应与源一致（2 个），源 %v 副本 %v", srcIDs, dstIDs)
	}
	old := map[string]bool{}
	for _, id := range srcIDs {
		old[id] = true
	}
	for _, id := range dstIDs {
		if old[id] {
			t.Fatalf("副本节点 ID %q 与源重复，必须全部重生成", id)
		}
	}
}
