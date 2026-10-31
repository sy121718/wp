package feature

import (
	"context"
	"strings"
	"testing"
	"time"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
)

func TestNavigationRespUpdatedAtIsPreciseToken(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()

	created, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "首页", Path: "/", Kind: "header",
	})
	if err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}

	token, terr := time.Parse(time.RFC3339Nano, created.UpdatedAt)
	if terr != nil {
		t.Fatalf("UpdatedAt 应为 RFC3339Nano 的乐观锁 token，实际 %q: %v", created.UpdatedAt, terr)
	}
	if !strings.Contains(created.UpdatedAt, "T") || token.Format(time.RFC3339Nano) != created.UpdatedAt {
		t.Fatalf("UpdatedAt 不应再是分钟展示串，实际 %q", created.UpdatedAt)
	}

	var stored time.Time
	if qerr := db.Raw("SELECT update_time FROM navigations WHERE id = ?", created.ID).
		Scan(&stored).Error; qerr != nil {
		t.Fatalf("读取库内 update_time 失败: %v", qerr)
	}
	if !token.Equal(stored) {
		t.Fatalf("Create 的 token 与库内不一致：token=%s 库内=%s",
			created.UpdatedAt, stored.Format(time.RFC3339Nano))
	}

	tree, err := navSvc.Tree(ctx, projectID, "header")
	if err != nil {
		t.Fatalf("读取导航树失败: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("导航树应有 1 个根节点，实际 %d", len(tree))
	}
	if tree[0].UpdatedAt != created.UpdatedAt {
		t.Errorf("树节点 token 与响应体不一致：tree=%s resp=%s",
			tree[0].UpdatedAt, created.UpdatedAt)
	}

	time.Sleep(2 * time.Millisecond)
	newTitle := "首页(改)"
	updated, err := navSvc.Update(ctx, &navigationdto.UpdateReq{
		ID: created.ID, Title: &newTitle, ExpectedUpdatedAt: &created.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("回带精确 token 的更新应成功，实际: %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("更新后标题应为 %q，实际 %q", newTitle, updated.Title)
	}
	if updated.UpdatedAt == created.UpdatedAt {
		t.Errorf("写后 token 应变化，实际仍是 %q", updated.UpdatedAt)
	}

	stale := created.UpdatedAt
	if _, err = navSvc.Update(ctx, &navigationdto.UpdateReq{
		ID: created.ID, Title: &newTitle, ExpectedUpdatedAt: &stale,
	}); err == nil || !strings.Contains(err.Error(), navigationenums.ErrStaleVersion) {
		t.Fatalf("过期 token 应报 %s，实际: %v", navigationenums.ErrStaleVersion, err)
	}

	got, err := navSvc.Get(ctx, &navigationdto.GetReq{ID: created.ID})
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	if got.UpdatedAt != updated.UpdatedAt {
		t.Errorf("Get 的 token 应为写后真值：got=%s want=%s", got.UpdatedAt, updated.UpdatedAt)
	}
	list, err := navSvc.List(ctx, &navigationdto.ListReq{ProjectID: projectID, Kind: "header"})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("列表应有 1 条，实际 %d", len(list))
	}
	if list[0].UpdatedAt != updated.UpdatedAt {
		t.Errorf("List 的 token 应为写后真值：list=%s want=%s", list[0].UpdatedAt, updated.UpdatedAt)
	}
	finalTitle := "首页(再改)"
	if _, err = navSvc.Update(ctx, &navigationdto.UpdateReq{
		ID: created.ID, Title: &finalTitle, ExpectedUpdatedAt: &list[0].UpdatedAt,
	}); err != nil {
		t.Fatalf("回带 List 给的 token 应能写成功，实际: %v", err)
	}
}
