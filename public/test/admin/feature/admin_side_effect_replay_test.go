package feature

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/casbin"
	"go_wp/pkg/facing"
	"go_wp/public/test/support"
)

func initAdminPolicyTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := casbin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := casbin.InitCasbin(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = casbin.Close() })
}

func sideEffectPending(t *testing.T, db *gorm.DB, ctx context.Context) int64 {
	t.Helper()
	var n int64
	if err := db.WithContext(ctx).Model(&adminmodel.SideEffectTaskEntity{}).
		Where("processed_time IS NULL").Count(&n).Error; err != nil {
		t.Fatalf("数待执行副作用任务失败: %v", err)
	}
	return n
}

func sideEffectTasks(t *testing.T, db *gorm.DB, ctx context.Context) []adminmodel.SideEffectTaskEntity {
	t.Helper()
	var rows []adminmodel.SideEffectTaskEntity
	if err := db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读副作用任务失败: %v", err)
	}
	return rows
}

// TestAdminSideEffectTaskRollsBackWithBusiness 证明任务行与业务写在同一事务：
// 业务被打回时，不能留下一条「要重载策略」的悬挂任务。
func TestAdminSideEffectTaskRollsBackWithBusiness(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	role := &adminmodel.RoleEntity{RoleCode: "sfx_role_x", RoleName: "副作用角色", IsSystem: 0}
	if err := db.WithContext(ctx).Create(role).Error; err != nil {
		t.Fatalf("造角色失败: %v", err)
	}

	before := sideEffectPending(t, db, ctx)

	const missing = uint64(99999998)
	err := svc.RoleDelete(ctx, &admindto.RoleDeleteReq{IDs: []uint64{role.ID, missing}})
	if err == nil {
		t.Fatal("混入不存在的 id 应被拒绝")
	}
	if got, ok := facing.StatusOf(err); !ok || got != http.StatusNotFound {
		t.Fatalf("状态码 = %d（声明 %v），want 404", got, ok)
	}

	after := sideEffectPending(t, db, ctx)
	if after != before {
		t.Fatalf("业务回滚后不应留下任务：before=%d after=%d，新增 %d 条", before, after, after-before)
	}
	if left := countRoleRows(t, db, ctx, role.ID); left != 1 {
		t.Fatalf("角色应仍在，实际 %d 行", left)
	}
}

// TestAdminSideEffectTaskIsPersistedAndReplayable 证明成功后落定、失败留痕可重放、重复执行幂等。
func TestAdminSideEffectTaskIsPersistedAndReplayable(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skip(err)
	}
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	roleA := &adminmodel.RoleEntity{RoleCode: "sfx_role_a", RoleName: "重放角色A", IsSystem: 0}
	if err := db.WithContext(ctx).Create(roleA).Error; err != nil {
		t.Fatalf("造角色失败: %v", err)
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := casbin.ActivateRoleTx(ctx, tx, roleA.RoleCode)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RoleDelete(ctx, &admindto.RoleDeleteReq{IDs: []uint64{roleA.ID}}); err != nil {
		t.Fatalf("删除应成功: %v", err)
	}

	tasks := sideEffectTasks(t, db, ctx)
	if len(tasks) == 0 {
		t.Fatal("删除后应留下至少一条副作用任务（策略重载）")
	}
	var found bool
	for _, task := range tasks {
		if task.Kind != adminmodel.SideEffectPolicyReload {
			continue
		}
		found = true
		if task.ProcessedTime == nil {
			t.Fatalf("任务 %d 提交后应立刻执行并标记完成，实际 processed_time 为空（last_error=%q）",
				task.ID, task.LastError)
		}
		if task.Attempts < 1 {
			t.Fatalf("任务 %d 至少应被领取过一次，实际 attempts=%d", task.ID, task.Attempts)
		}
		if task.LastError != "" {
			t.Fatalf("成功执行的任务 %d 不应留下错误：%q", task.ID, task.LastError)
		}
	}
	if !found {
		t.Fatalf("应有 kind=%s 的任务，实际 %+v", adminmodel.SideEffectPolicyReload, tasks)
	}

	t.Run("再跑一轮不重复执行已完成的任务", func(t *testing.T) {
		done, err := svc.ReplaySideEffects(ctx)
		if err != nil {
			t.Fatalf("重放失败: %v", err)
		}
		if done != 0 {
			t.Fatalf("已完成的任务不应被再次领取，实际领取 %d 条", done)
		}
	})

	t.Run("失败任务保留 attempts 与 last_error 且可被再次领取", func(t *testing.T) {
		// 直接写一条「payload 非法」的吊销任务：执行必然失败，用来验证失败留痕路径。
		var id int64
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			row := adminmodel.SideEffectTaskEntity{
				Kind:    adminmodel.SideEffectRevokeSession,
				Payload: "{not-json",
			}
			if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
				return err
			}
			id = row.ID
			return nil
		})
		if err != nil {
			t.Fatalf("写失败任务失败: %v", err)
		}

		if _, err := svc.ReplaySideEffects(ctx); err != nil {
			t.Fatalf("重放失败: %v", err)
		}

		var row adminmodel.SideEffectTaskEntity
		if err := db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
			t.Fatalf("读回失败任务失败: %v", err)
		}
		if row.ProcessedTime != nil {
			t.Fatalf("失败任务不应被标记完成")
		}
		if row.Attempts < 1 {
			t.Fatalf("失败任务应记录领取次数，实际 attempts=%d", row.Attempts)
		}
		if row.LastError == "" {
			t.Fatal("失败任务必须留下 last_error 供定位")
		}

		// 修好 payload 后再跑一轮：同一个 id 应能被重新领取并完成 —— 证明可重放。
		if err := db.WithContext(ctx).Model(&adminmodel.SideEffectTaskEntity{}).
			Where("id = ?", id).
			Updates(map[string]any{"payload": `{"user_id":1}`, "claimed_time": nil}).Error; err != nil {
			t.Fatalf("修 payload 失败: %v", err)
		}
		if _, err := svc.ReplaySideEffects(ctx); err != nil {
			t.Fatalf("第二轮重放失败: %v", err)
		}
		var again adminmodel.SideEffectTaskEntity
		if err := db.WithContext(ctx).Where("id = ?", id).First(&again).Error; err != nil {
			t.Fatalf("第二轮读回失败: %v", err)
		}
		if again.ProcessedTime == nil {
			t.Fatalf("修好后的任务应被重放成功，实际仍未完成（attempts=%d last_error=%q）",
				again.Attempts, again.LastError)
		}
	})
}

func countRoleRows(t *testing.T, db *gorm.DB, ctx context.Context, id uint64) int64 {
	t.Helper()
	var n int64
	if err := db.WithContext(ctx).Table("sys_role").Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatalf("数角色行失败: %v", err)
	}
	return n
}

// failNextInserts 在指定表的第 n 次 INSERT 上返回受控错误。
//
// 故障注入只在测试侧：注册到这次测试用的 db 连接上，用完摘掉，
// 不往生产 service 加任何「专为测试的失败开关」。
func failNextInserts(t *testing.T, db *gorm.DB, table string, n int) {
	t.Helper()
	var seen int
	cb := db.Callback().Create()
	const name = "test:inject_insert_failure"
	if err := cb.Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != table {
			return
		}
		seen++
		if seen == n {
			_ = tx.AddError(errors.New("注入的持久化故障"))
		}
	}); err != nil {
		t.Fatalf("注册故障注入回调失败: %v", err)
	}
	t.Cleanup(func() { _ = cb.Remove(name) })
}

// TestAdminSideEffectTaskRollsBackAfterAppend 证明「任务已经写进事务」之后发生故障时，
// 任务行与业务行一起回滚 —— 这是任务与业务同事务的实质证据，
// 不是「前置校验失败所以没写」那种弱断言。
func TestAdminSideEffectTaskRollsBackAfterAppend(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	adminA := &adminmodel.AdminEntity{Username: "rb_admin_a", Password: "x", Status: adminmodel.AdminStatusActive}
	adminB := &adminmodel.AdminEntity{Username: "rb_admin_b", Password: "x", Status: adminmodel.AdminStatusActive}
	for _, e := range []*adminmodel.AdminEntity{adminA, adminB} {
		if err := db.WithContext(ctx).Create(e).Error; err != nil {
			t.Fatalf("造管理员失败: %v", err)
		}
	}

	before := sideEffectPending(t, db, ctx)

	// AdminBulkDelete 的事务里，删除之后依次写：1 条重载任务、每个 id 一条吊销任务。
	// 让第 2 条 INSERT 失败 —— 此时重载任务已经在事务里写成功了。
	failNextInserts(t, db, "admin_side_effect_tasks", 2)

	_, err := svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id: []uint64{adminA.ID, adminB.ID}, OperatorID: 0,
	})
	if err == nil {
		t.Fatal("注入故障后删除必须失败")
	}
	if !strings.Contains(err.Error(), "注入的持久化故障") {
		t.Fatalf("错误应透出注入的故障原因，实际 %v", err)
	}

	// 关键：业务行没删，任务行也没留下。
	if left := countAdminRows(t, db, ctx, adminA.ID); left != 1 {
		t.Fatalf("回滚后管理员 A 应仍在，实际 %d 行", left)
	}
	if left := countAdminRows(t, db, ctx, adminB.ID); left != 1 {
		t.Fatalf("回滚后管理员 B 应仍在，实际 %d 行", left)
	}
	if after := sideEffectPending(t, db, ctx); after != before {
		t.Fatalf("回滚后不应留下副作用任务：before=%d after=%d", before, after)
	}
}

func countAdminRows(t *testing.T, db *gorm.DB, ctx context.Context, id uint64) int64 {
	t.Helper()
	var n int64
	if err := db.WithContext(ctx).Table("sys_admin").Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatalf("数管理员行失败: %v", err)
	}
	return n
}

// TestSideEffectWorkerStopsWhenContextCancelled 证明循环真的能被 ctx 停掉：
// 取消后不再领取新任务，也不继续写库。
func TestSideEffectWorkerStopsWhenContextCancelled(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx, cancel := context.WithCancel(context.Background())

	// 放一条可执行任务，验证 worker 起来会先消化掉它。
	if err := db.WithContext(ctx).Create(&adminmodel.SideEffectTaskEntity{
		Kind: adminmodel.SideEffectPolicyReload, Payload: "{}",
	}).Error; err != nil {
		t.Fatalf("写任务失败: %v", err)
	}
	before := sideEffectPending(t, db, ctx)
	if before == 0 {
		t.Fatal("前置：应有待执行任务")
	}

	svc.StartSideEffectWorker(ctx, 10*time.Millisecond)
	waitUntil(t, 2*time.Second, func() bool {
		return sideEffectPending(t, db, context.Background()) == 0
	})

	// 停掉之后再放一条任务：没有人应该去领它。
	cancel()
	time.Sleep(50 * time.Millisecond)

	if err := db.WithContext(context.Background()).Create(&adminmodel.SideEffectTaskEntity{
		Kind: adminmodel.SideEffectPolicyReload, Payload: "{}",
	}).Error; err != nil {
		t.Fatalf("写第二条任务失败: %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	if left := sideEffectPending(t, db, context.Background()); left != 1 {
		t.Fatalf("ctx 取消后 worker 不应再领取任务，实际待执行 %d 条", left)
	}
}

func waitUntil(t *testing.T, budget time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%v 内条件未满足", budget)
}

// TestFlushSideEffectClaimsItsOwnTaskOnly 证明提交后的「立刻做一次」只领自己那一条。
//
// 反例（改前）：FlushSideEffect 走批量领取（按 id 升序取 1 条）。队列里只要躺着一条
// 更早的失败任务，这次刚登记的任务就会被推到下一轮 —— 调用方以为已经执行过了。
// 这里先造一条注定失败的任务（payload 非法），再登记一条正常任务并立刻 Flush：
// 失败那条必须原样留在队列里，正常那条必须当场完成。
func TestFlushSideEffectClaimsItsOwnTaskOnly(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	// 先塞一条永远会失败的旧任务（payload 不是合法 JSON），并把它标记成「失败待重试」。
	stale := &adminmodel.SideEffectTaskEntity{
		Kind:    adminmodel.SideEffectRevokeSession,
		Payload: "{不是合法 JSON",
	}
	if err := db.WithContext(ctx).Create(stale).Error; err != nil {
		t.Fatalf("造失败任务失败: %v", err)
	}
	if _, err := svc.ReplaySideEffects(ctx); err != nil {
		t.Fatalf("首轮重放不该报错（失败留在任务行）: %v", err)
	}
	var staleAfter adminmodel.SideEffectTaskEntity
	if err := db.WithContext(ctx).First(&staleAfter, stale.ID).Error; err != nil {
		t.Fatalf("读回失败任务失败: %v", err)
	}
	if staleAfter.ProcessedTime != nil || staleAfter.LastError == "" {
		t.Fatalf("前置失败任务状态不对：processed=%v last_error=%q", staleAfter.ProcessedTime, staleAfter.LastError)
	}

	// 再登记一条正常任务：策略重载。
	reloadID, err := svc.AppendPolicyReloadForTest(ctx)
	if err != nil {
		t.Fatalf("登记任务失败: %v", err)
	}
	if reloadID <= stale.ID {
		t.Fatalf("新任务 id 应大于旧任务：new=%d old=%d", reloadID, stale.ID)
	}

	svc.FlushSideEffect(ctx, reloadID)

	var reloadAfter adminmodel.SideEffectTaskEntity
	if err := db.WithContext(ctx).First(&reloadAfter, reloadID).Error; err != nil {
		t.Fatalf("读回新任务失败: %v", err)
	}
	if reloadAfter.ProcessedTime == nil {
		t.Fatalf("指定的新任务没有被立刻执行（旧失败任务把队首占了）：%+v", reloadAfter)
	}

	var staleFinal adminmodel.SideEffectTaskEntity
	if err := db.WithContext(ctx).First(&staleFinal, stale.ID).Error; err != nil {
		t.Fatalf("读回失败任务失败: %v", err)
	}
	if staleFinal.ProcessedTime != nil {
		t.Fatal("旧失败任务不该被这次 Flush 处理掉")
	}
	if staleFinal.Attempts != staleAfter.Attempts {
		t.Fatalf("旧任务 attempts 被这次 Flush 改动：before=%d after=%d", staleAfter.Attempts, staleFinal.Attempts)
	}
}

// TestSideEffectReleaseDoesNotClearNewerLease 证明旧持有者写回时既不会清掉新持有者的租约，
// 也不会把已被别人做完的任务重新标成完成 —— 用 attempts 做租约令牌，靠条件更新挡住。
//
// 场景：一次执行超过租约时长（2 分钟），期间另一个执行者把同一行重领了。
// 旧持有者收尾写「失败 + claimed_time=NULL」时若不带 attempts 条件，就会清空新持有者的租约，
// 让第三个人再领一次（副作用重复执行）；写「完成」时不带条件，则会把别人刚领走的行提前判完。
func TestSideEffectReleaseDoesNotClearNewerLease(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	ctx := context.Background()
	st := adminmodel.NewSideEffectTaskModel(db)

	var taskID int64
	if err := st.Transaction(ctx, func(tx *gorm.DB) error {
		var e error
		taskID, e = st.AppendTx(ctx, tx, adminmodel.SideEffectPolicyReload, "{}")
		return e
	}); err != nil {
		t.Fatalf("登记任务失败: %v", err)
	}

	first, err := st.ClaimOneTxInTest(ctx, taskID, time.Minute)
	if err != nil || !first.Ok {
		t.Fatalf("第一次领取失败：ok=%v err=%v", first.Ok, err)
	}

	// 模拟「租约到期后别人重领」：把 claimed_time 推到很久以前，再领一次。
	if err := db.WithContext(ctx).Model(&adminmodel.SideEffectTaskEntity{}).
		Where("id = ?", taskID).
		Update("claimed_time", gorm.Expr("now() - interval '10 minutes'")).Error; err != nil {
		t.Fatalf("制造过期租约失败: %v", err)
	}
	second, err := st.ClaimOneTxInTest(ctx, taskID, time.Minute)
	if err != nil || !second.Ok {
		t.Fatalf("第二次领取失败：ok=%v err=%v", second.Ok, err)
	}
	if second.Task.Attempts <= first.Task.Attempts {
		t.Fatalf("attempts 应递增：first=%d second=%d", first.Task.Attempts, second.Task.Attempts)
	}

	// ① 旧持有者写「失败」：必须打不中任何行，新持有者的租约还在。
	if err := st.MarkFailed(ctx, taskID, first.Task.Attempts, "旧持有者的失败"); err != nil {
		t.Fatalf("旧持有者写回失败: %v", err)
	}
	var row adminmodel.SideEffectTaskEntity
	if err := db.WithContext(ctx).First(&row, taskID).Error; err != nil {
		t.Fatalf("读回任务失败: %v", err)
	}
	if row.ClaimedTime == nil {
		t.Fatal("旧持有者把新持有者的租约清掉了：claimed_time 变成 NULL，任务会被第三个人再领一次")
	}
	if row.Attempts != second.Task.Attempts {
		t.Fatalf("attempts 被旧持有者改写：want %d got %d", second.Task.Attempts, row.Attempts)
	}
	if row.ProcessedTime != nil {
		t.Fatal("旧持有者的失败写回不该把任务标记成完成")
	}

	// ② 旧持有者写「完成」：同样必须打不中，否则别人正在执行的行会被提前判完。
	if err := st.MarkDone(ctx, taskID, first.Task.Attempts); err != nil {
		t.Fatalf("旧持有者写完成失败: %v", err)
	}
	if err := db.WithContext(ctx).First(&row, taskID).Error; err != nil {
		t.Fatalf("读回任务失败: %v", err)
	}
	if row.ProcessedTime != nil {
		t.Fatal("旧持有者把别人正持有的任务标记成了完成（条件更新失效）")
	}

	// ③ 新持有者收尾写「完成」：必须打中，任务落地完成。
	if err := st.MarkDone(ctx, taskID, second.Task.Attempts); err != nil {
		t.Fatalf("新持有者写回失败: %v", err)
	}
	if err := db.WithContext(ctx).First(&row, taskID).Error; err != nil {
		t.Fatalf("读回任务失败: %v", err)
	}
	if row.ProcessedTime == nil {
		t.Fatal("新持有者的完成状态没有写回")
	}
}
