package unit

// list_stale_page_ids_test.go — 组件升级后的版本比对：判定集合完全由调用方给出（报告 ARCH-03）。
//
// 这条契约的**形状**本身就是整改内容：artifact 只回答「这一行产物是哪个版本产出的」，
// 「哪一行算当前产物」由来源模块（page / presentation 的语言账本）决定。旧签名不接受集合，
// 于是它只能扫 page_artifacts 里全部 available 行 —— 未 GC 的历史回滚产物因此也进了判定，
// 页面重建之后每次重启都会被重新标成 stale。

import (
	"context"
	"testing"
)

// TestListStalePageIDsJudgesOnlyGivenArtifacts 只有调用方传进来的「当前产物」参与比对。
func TestListStalePageIDsJudgesOnlyGivenArtifacts(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	// 历史产物：上一轮组件产出、已回滚但仍 available（未 GC）。
	old := validReq()
	old.ArtifactID = testArtifactID
	old.RegistryVersion = "reg-old"
	old.ArtifactHash = artifactHashV1
	mustRecord(t, svc, old)

	// 当前产物：重建后新组件产出。
	cur := validReq()
	cur.ArtifactID = testArtifactID2
	cur.Version = 2
	cur.RegistryVersion = "reg-new"
	cur.ArtifactHash = artifactHashV2
	mustRecord(t, svc, cur)

	// ① 只传当前产物：历史行不参与 → 没有差异。
	ids, err := svc.ListStalePageIDs(ctx, "reg-new", []string{testArtifactID2})
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("未被调用方判为当前产物的历史行不该参与比对，实际 %v", ids)
	}

	// ② 把历史行一并传进来（旧实现「扫全部 available 行」的口径）：必须命中 ——
	// 证明命中面完全由调用方给的集合决定，而不是由表里留了多少历史行决定。
	ids, err = svc.ListStalePageIDs(ctx, "reg-new", []string{testArtifactID, testArtifactID2})
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != testPageID {
		t.Fatalf("集合里含旧版本产物时应命中该页 %s，实际 %v", testPageID, ids)
	}

	// ③ 当前产物确实由旧组件产出（账本还指在旧行上）→ 命中。
	ids, err = svc.ListStalePageIDs(ctx, "reg-new", []string{testArtifactID})
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != testPageID {
		t.Fatalf("当前产物由旧组件产出时必须命中 %s，实际 %v", testPageID, ids)
	}

	// ④ 空集合：调用方没有当前产物（如全站尚无激活产物）→ 不做任何判定，
	// 不允许退化成「自己扫全表」。
	if ids, err = svc.ListStalePageIDs(ctx, "reg-new", nil); err != nil || len(ids) != 0 {
		t.Fatalf("空集合不该命中任何页：ids=%v err=%v", ids, err)
	}

	// ⑤ current 为空（二进制无 VCS 信息）：宁可不标也不全站误标。
	if ids, err = svc.ListStalePageIDs(ctx, "", []string{testArtifactID}); err != nil || len(ids) != 0 {
		t.Fatalf("current 为空时不该命中任何页：ids=%v err=%v", ids, err)
	}
}
