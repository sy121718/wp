package feature

// navigation_resp_token_test.go — JSON API 路径的乐观锁 token（NavigationResp.UpdatedAt）。
//
// 背景：乐观锁 token 是「精确到微秒的 update_time」的 RFC3339Nano 串，UpdateReq.ExpectedUpdatedAt
// 原样回带。此前 NavigationResp.UpdatedAt 发的是给人看的分钟串，只有树节点
// NavigationNode.UpdatedAt 是精确值 —— 走 JSON API（POST /api/navigation/create|update、
// GET /api/navigation/get|list）的消费者拿不到可比对的值，只能自己拼秒级串，而 update_time
// 是微秒精度，秒级串只在恰好落在整秒时命中：乐观锁要么静默失效（该拦的没拦），要么每次都
// 误报冲突（该过的过不去）。字段收敛成一个精确值之后，「拿展示串当 token」的陷阱不再存在，
// 本文件把这条链路钉住。
//
// 为什么放在 feature 而不是模块内单测：这里要断言的正是**库列的真实精度**
// （timestamptz(6) 对纳秒取整）。若用 AutoMigrate 自建表，列精度由 gorm 标签推断，
// 与生产 DDL 静默分叉 —— 那恰好是 AGENTS.md 点名的失效形态。

import (
	"context"
	"strings"
	"testing"
	"time"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
)

// TestNavigationRespUpdatedAtIsPreciseToken JSON API 的响应带可用作乐观锁 token 的精确值。
func TestNavigationRespUpdatedAtIsPreciseToken(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()

	created, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "首页", Path: "/", Kind: "header",
	})
	if err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}

	// 1) 必须是可直接比对的精确值：RFC3339Nano 可解析、能原样往返，且**不再是**分钟展示串
	//   （分钟串没有 T 分隔符；拿它回带 ExpectedUpdatedAt 永远比不中）。
	token, terr := time.Parse(time.RFC3339Nano, created.UpdatedAt)
	if terr != nil {
		t.Fatalf("UpdatedAt 应为 RFC3339Nano 的乐观锁 token，实际 %q: %v", created.UpdatedAt, terr)
	}
	if !strings.Contains(created.UpdatedAt, "T") || token.Format(time.RFC3339Nano) != created.UpdatedAt {
		t.Fatalf("UpdatedAt 不应再是分钟展示串，实际 %q", created.UpdatedAt)
	}

	// 2) token 必须等于**库内真值**：Create 若直接返回内存里的 time.Now()（纳秒），
	//    与微秒列比对就不相等，调用方刚建好就报版本冲突。
	var stored time.Time
	if qerr := db.Raw("SELECT update_time FROM navigations WHERE id = ?", created.ID).
		Scan(&stored).Error; qerr != nil {
		t.Fatalf("读取库内 update_time 失败: %v", qerr)
	}
	if !token.Equal(stored) {
		t.Fatalf("Create 的 token 与库内不一致：token=%s 库内=%s",
			created.UpdatedAt, stored.Format(time.RFC3339Nano))
	}

	// 3) 树节点与响应体必须是同一个 token（两条入口不能各给一个口径 —— 收敛前正是这里
	//    同名两义：树节点给精确值、响应体给分钟串）。
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

	// 4) 回带 token 能完成一次乐观锁写；同一 token 再用一次必须被拒（不是静默覆盖）。
	//    先跨过一个微秒边界，保证第 5 步的失败只可能来自版本变化，而不是时钟粒度。
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

	// 5) Get / List 两个读路径给的 token 同样能直接写回。
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
