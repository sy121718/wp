package unit

import (
	"context"
	"testing"
)

func TestListStalePageIDsJudgesOnlyGivenArtifacts(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	old := validReq()
	old.ArtifactID = testArtifactID
	old.RegistryVersion = "reg-old"
	old.ArtifactHash = artifactHashV1
	mustRecord(t, svc, old)

	cur := validReq()
	cur.ArtifactID = testArtifactID2
	cur.Version = 2
	cur.RegistryVersion = "reg-new"
	cur.ArtifactHash = artifactHashV2
	mustRecord(t, svc, cur)

	ids, err := svc.ListStalePageIDs(ctx, "reg-new", []string{testArtifactID2})
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("未被调用方判为当前产物的历史行不该参与比对，实际 %v", ids)
	}

	ids, err = svc.ListStalePageIDs(ctx, "reg-new", []string{testArtifactID, testArtifactID2})
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != testPageID {
		t.Fatalf("集合里含旧版本产物时应命中该页 %s，实际 %v", testPageID, ids)
	}

	ids, err = svc.ListStalePageIDs(ctx, "reg-new", []string{testArtifactID})
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != testPageID {
		t.Fatalf("当前产物由旧组件产出时必须命中 %s，实际 %v", testPageID, ids)
	}

	if ids, err = svc.ListStalePageIDs(ctx, "reg-new", nil); err != nil || len(ids) != 0 {
		t.Fatalf("空集合不该命中任何页：ids=%v err=%v", ids, err)
	}

	if ids, err = svc.ListStalePageIDs(ctx, "", []string{testArtifactID}); err != nil || len(ids) != 0 {
		t.Fatalf("current 为空时不该命中任何页：ids=%v err=%v", ids, err)
	}
}
