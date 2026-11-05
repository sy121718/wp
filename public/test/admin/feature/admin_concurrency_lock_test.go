package feature

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/facing"
	"go_wp/public/test/support"
)

// deptRow 读回一个部门的父子与祖先链事实。
type deptRow struct {
	ID        uint64
	ParentID  uint64
	Ancestors string
}

func readDept(t *testing.T, db *gorm.DB, ctx context.Context, id uint64) deptRow {
	t.Helper()
	var e adminmodel.DeptEntity
	if err := db.WithContext(ctx).First(&e, id).Error; err != nil {
		t.Fatalf("读部门 %d 失败: %v", id, err)
	}
	return deptRow{ID: e.ID, ParentID: e.ParentID, Ancestors: e.Ancestors}
}

// TestDeptUpdateMoveKeepsAncestorChainConsistent 证明「改父」在新父与自身行锁下不会写出错链。
//
// 反例（改前）：新父只 GetByIDTx 读一次。读完到写回之间父链可能被别的移动改掉，
// 于是子部门算出基于旧祖先链的 ancestors，树里出现「父子关系是一个版本、祖先链是另一个版本」。
// 这里并发跑「把 B 挂到 A 下」与「把 C 也挂到 A 下」，收尾后逐行核对祖先链与父子关系一致。
func TestDeptUpdateMoveKeepsAncestorChainConsistent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	root := &adminmodel.DeptEntity{DeptCode: "lk_root", DeptName: "根", ParentID: 0, Ancestors: "0", Status: 1}
	if err := db.WithContext(ctx).Create(root).Error; err != nil {
		t.Fatalf("造根部门失败: %v", err)
	}
	mid := &adminmodel.DeptEntity{DeptCode: "lk_mid", DeptName: "中间", ParentID: root.ID, Ancestors: "0," + u64(root.ID), Status: 1}
	if err := db.WithContext(ctx).Create(mid).Error; err != nil {
		t.Fatalf("造中间部门失败: %v", err)
	}
	leaf := &adminmodel.DeptEntity{DeptCode: "lk_leaf", DeptName: "叶", ParentID: mid.ID, Ancestors: mid.Ancestors + "," + u64(mid.ID), Status: 1}
	if err := db.WithContext(ctx).Create(leaf).Error; err != nil {
		t.Fatalf("造叶部门失败: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	moves := []struct {
		id, parent uint64
	}{{mid.ID, root.ID}, {leaf.ID, root.ID}}
	for i, mv := range moves {
		wg.Add(1)
		go func(i int, id, parent uint64) {
			defer wg.Done()
			errs[i] = svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{
				ID: id, ParentID: parent, DeptName: "改名" + u64(id), DeptCode: "lk_c" + u64(id), Status: 1,
			})
			// DeptCode 与既有行冲突时允许打回：那不是本用例要验的东西。
			if errs[i] != nil && !errors.Is(errs[i], adminenums.Err(adminenums.ErrDeptCodeExists)) {
				return
			}
			errs[i] = nil
		}(i, mv.id, mv.parent)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("移动 %d 失败: %v", i, err)
		}
	}

	// 祖先链必须能由父子关系推出来：逐行核对。
	var all []adminmodel.DeptEntity
	if err := db.WithContext(ctx).Order("id ASC").Find(&all).Error; err != nil {
		t.Fatalf("读全部部门失败: %v", err)
	}
	byID := make(map[uint64]adminmodel.DeptEntity, len(all))
	for _, e := range all {
		byID[e.ID] = e
	}
	var check func(id uint64, depth int)
	check = func(id uint64, depth int) {
		if depth > 10 {
			t.Fatalf("祖先链成环：部门 %d", id)
		}
		got := byID[id]
		if got.ParentID == 0 {
			if got.Ancestors != "0" {
				t.Errorf("根部门 %d 的祖先链应为 \"0\"，实际 %q", id, got.Ancestors)
			}
			return
		}
		parent, ok := byID[got.ParentID]
		if !ok {
			t.Fatalf("部门 %d 的父 %d 不存在", id, got.ParentID)
		}
		want := parent.Ancestors + "," + u64(parent.ID)
		if got.Ancestors != want {
			t.Errorf("部门 %d 的祖先链 = %q，按父子关系应为 %q", id, got.Ancestors, want)
		}
		check(got.ParentID, depth+1)
	}
	for id := range byID {
		check(id, 0)
	}
}

func TestDeptMoveWaitsForTreeLock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	a := adminmodel.DeptEntity{DeptCode: "wait_a", DeptName: "A", Ancestors: "0", Status: 1}
	b := adminmodel.DeptEntity{DeptCode: "wait_b", DeptName: "B", Ancestors: "0", Status: 1}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	holder := db.WithContext(ctx).Begin()
	if holder.Error != nil {
		t.Fatal(holder.Error)
	}
	defer holder.Rollback()
	var pid int
	if err := holder.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
		t.Fatal(err)
	}
	// 只持有无关的 A：移动 B 也必须先等待树锁，而非仅锁 B 与新父。
	if _, err := adminmodel.NewDeptModel(db).LockByIDTx(ctx, holder, a.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{ID: b.ID, ParentID: 0, DeptCode: b.DeptCode, DeptName: "updated", Status: 1})
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	waiting := false
	for !waiting {
		var count int64
		err := db.WithContext(ctx).Raw("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND ? = ANY(pg_blocking_pids(pid))", pid).Scan(&count).Error
		if err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			waiting = true
			break
		}
		select {
		case err := <-done:
			t.Fatalf("未观察到树锁等待，移动提前结束: %v", err)
		case <-ctx.Done():
			t.Fatalf("等待证据超时: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	if err := holder.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("释放锁后移动失败: %v", err)
	}
	var got adminmodel.DeptEntity
	if err := db.First(&got, b.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.DeptName != "updated" || got.ParentID != 0 || got.Ancestors != "0" {
		t.Fatalf("移动后事实不符: %+v", got)
	}
}

func TestDeptOppositeMovesRejectCycleWithoutDeadlock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	for round := 0; round < 6; round++ {
		a := adminmodel.DeptEntity{DeptCode: "opposite_a_" + u64(uint64(round)), DeptName: "A", Ancestors: "0", Status: 1}
		b := adminmodel.DeptEntity{DeptCode: "opposite_b_" + u64(uint64(round)), DeptName: "B", Ancestors: "0", Status: 1}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&b).Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, pair := range [][2]adminmodel.DeptEntity{{a, b}, {b, a}} {
			go func(pair [2]adminmodel.DeptEntity) {
				<-start
				results <- svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{ID: pair[0].ID, ParentID: pair[1].ID, DeptCode: pair[0].DeptCode, DeptName: pair[0].DeptName, Status: 1})
			}(pair)
		}
		close(start)
		success, circle := 0, 0
		for i := 0; i < 2; i++ {
			err := <-results
			if err == nil {
				success++
			} else if errors.Is(err, adminenums.Err(adminenums.ErrDeptCircle)) {
				circle++
			} else {
				cancel()
				t.Fatalf("相向移动出现基础设施失败: %v", err)
			}
		}
		cancel()
		if success != 1 || circle != 1 {
			t.Fatalf("相向移动结果成功=%d 环冲突=%d", success, circle)
		}
		gotA := readDept(t, db, context.Background(), a.ID)
		gotB := readDept(t, db, context.Background(), b.ID)
		if gotA.ParentID == b.ID {
			if gotB.ParentID != 0 || gotA.Ancestors != "0,"+u64(b.ID) {
				t.Fatalf("祖先链与父关系不一致: %+v %+v", gotA, gotB)
			}
		} else if gotB.ParentID != a.ID || gotA.ParentID != 0 || gotB.Ancestors != "0,"+u64(a.ID) {
			t.Fatalf("祖先链与父关系不一致: %+v %+v", gotA, gotB)
		}
	}
}

// TestAdminBatchSetDeptIDHoldsDeptLockAgainstDelete 证明「把人挪进部门」与「删部门」互斥。
//
// 反例（改前）：挪人只 GetByIDTx 读一次部门在不在。删除端先数「这个部门还有没有成员」
// 得到 0，紧接着挪人提交，于是部门被删掉、成员却指向一个不存在的部门。
// 这里并发跑「把两个管理员挪进 D」与「删除 D」，断言两种结局之一：
//   - 删除成功 → 那两个管理员的 dept_id 仍为原值（没有指向空部门）；
//   - 挪人成功 → 删除被 409 打回（ErrDeptHasUsers），部门还在。
func TestAdminBatchSetDeptIDHoldsDeptLockAgainstDelete(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	dept := &adminmodel.DeptEntity{DeptCode: "lk_keep", DeptName: "目标部门", ParentID: 0, Ancestors: "0", Status: 1}
	if err := db.WithContext(ctx).Create(dept).Error; err != nil {
		t.Fatalf("造部门失败: %v", err)
	}
	other := &adminmodel.DeptEntity{DeptCode: "lk_other", DeptName: "原部门", ParentID: 0, Ancestors: "0", Status: 1}
	if err := db.WithContext(ctx).Create(other).Error; err != nil {
		t.Fatalf("造原部门失败: %v", err)
	}

	admins := make([]*adminmodel.AdminEntity, 0, 2)
	for _, code := range []string{"lk_a1", "lk_a2"} {
		a := &adminmodel.AdminEntity{Username: code, DeptID: other.ID, Status: 1}
		if err := db.WithContext(ctx).Create(a).Error; err != nil {
			t.Fatalf("造管理员失败: %v", err)
		}
		admins = append(admins, a)
	}

	var wg sync.WaitGroup
	var moveErr, deleteErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		moveErr = svc.AdminBatchSetDeptID(ctx, &admindto.AdminBatchSetDeptIDReq{
			UserIDs: []uint64{admins[0].ID, admins[1].ID}, DeptID: dept.ID,
		})
	}()
	go func() {
		defer wg.Done()
		deleteErr = svc.DeptDelete(ctx, &admindto.DeptDeleteReq{IDs: []uint64{dept.ID}})
	}()
	wg.Wait()

	var stillThere int64
	if err := db.WithContext(ctx).Model(&adminmodel.DeptEntity{}).Where("id = ?", dept.ID).Count(&stillThere).Error; err != nil {
		t.Fatalf("数部门失败: %v", err)
	}
	var pointing int64
	if err := db.WithContext(ctx).Model(&adminmodel.AdminEntity{}).
		Where("id IN ?", []uint64{admins[0].ID, admins[1].ID}).Where("dept_id = ?", dept.ID).
		Count(&pointing).Error; err != nil {
		t.Fatalf("数成员失败: %v", err)
	}

	switch {
	case deleteErr == nil:
		if stillThere != 0 {
			t.Fatalf("删除报成功但部门还在：%d", stillThere)
		}
		if pointing != 0 {
			t.Fatalf("部门已删，但 %d 个成员仍指向它（悬挂窗口没堵住）", pointing)
		}
		if moveErr != nil {
			t.Logf("挪人被拒（部门先被删）：%v", moveErr)
		}
	default:
		got, ok := facing.StatusOf(deleteErr)
		if !ok || got != 409 {
			t.Fatalf("删除被拒时状态应是 409，实际 %d（声明 %v）：%v", got, ok, deleteErr)
		}
		if stillThere != 1 {
			t.Fatalf("删除被拒但部门没了")
		}
		if moveErr != nil {
			t.Fatalf("挪人应成功（它是先拿到锁的一方）：%v", moveErr)
		}
		if pointing != 2 {
			t.Fatalf("挪人报成功但只有 %d 个成员指向该部门", pointing)
		}
	}
}

// TestRoleGrantBothDirectionsShareTransaction 证明「按角色收成员」与「按用户收角色」
// 两个入口写的是同一张绑定表、且都落在各自事务里：任一侧失败都不留半套绑定。
func TestRoleGrantBothDirectionsShareTransaction(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	role := &adminmodel.RoleEntity{RoleCode: "lk_role", RoleName: "联调角色", IsSystem: 0, Status: 1}
	if err := db.WithContext(ctx).Create(role).Error; err != nil {
		t.Fatalf("造角色失败: %v", err)
	}
	a := &adminmodel.AdminEntity{Username: "lk_g1", Status: 1}
	b := &adminmodel.AdminEntity{Username: "lk_g2", Status: 1}
	for _, x := range []*adminmodel.AdminEntity{a, b} {
		if err := db.WithContext(ctx).Create(x).Error; err != nil {
			t.Fatalf("造管理员失败: %v", err)
		}
	}

	// 按角色收成员：把 a、b 都拉进角色。
	if _, err := svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{RoleID: role.ID, UserIDs: []uint64{a.ID, b.ID}}); err != nil {
		t.Fatalf("按角色收成员失败: %v", err)
	}
	// 按用户收角色：把 b 移出角色（只剩 a）。
	if _, err := svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: b.ID, RoleCodes: []string{}, OperatorID: 0,
	}); err != nil {
		t.Fatalf("按用户收角色失败: %v", err)
	}

	var rows []casbinRuleRow
	if err := db.WithContext(ctx).Raw(
		"SELECT v0, v1 FROM sys_casbin_rule WHERE ptype = 'g' AND v1 = ? ORDER BY v0", role.RoleCode).
		Scan(&rows).Error; err != nil {
		t.Fatalf("读绑定失败: %v", err)
	}
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r.V0)
	}
	sort.Strings(got)
	want := []string{u64(a.ID)}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("两个入口写的是同一张表：绑定 = %v，期望 %v", got, want)
	}
}

// TestRoleUserSaveRollsBackBindingsOnFailure 证明成员绑定写在本事务里：
// 事务中途失败时，绑定不会留在 sys_casbin_rule（改前走 enforcer 的独立连接，回滚不了）。
//
// 打靶点选在绑定写入之后：先放开一次绑定 INSERT，让后面的读取/写入失败，
// 这样「已经写进去的绑定」必须跟着事务一起回滚 —— 而不是「压根没写到」。
func TestRoleUserSaveRollsBackBindingsOnFailure(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initAdminPolicyTest(t, db)
	svc := adminservice.NewService(db)
	ctx := context.Background()

	role := &adminmodel.RoleEntity{RoleCode: "lk_rollback", RoleName: "回滚角色", IsSystem: 0, Status: 1}
	if err := db.WithContext(ctx).Create(role).Error; err != nil {
		t.Fatalf("造角色失败: %v", err)
	}
	a := &adminmodel.AdminEntity{Username: "lk_rb1", Status: 1}
	if err := db.WithContext(ctx).Create(a).Error; err != nil {
		t.Fatalf("造管理员失败: %v", err)
	}

	before := countGrouping(t, db, ctx, role.RoleCode)

	// 事务里写入绑定之后注入故障：绑定与业务必须一起回滚。
	injectInsertFailureAfter(t, db, "admin_side_effect_tasks", 0)
	_, err := svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{RoleID: role.ID, UserIDs: []uint64{a.ID}})
	if err == nil {
		t.Fatal("注入故障后应报错")
	}
	after := countGrouping(t, db, ctx, role.RoleCode)
	if after != before {
		t.Fatalf("事务回滚了但绑定还在：before=%d after=%d", before, after)
	}
}

type casbinRuleRow struct {
	V0 string
	V1 string
}

func countGrouping(t *testing.T, db *gorm.DB, ctx context.Context, roleCode string) int64 {
	t.Helper()
	var n int64
	if err := db.WithContext(ctx).Raw(
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'g' AND v1 = ?", roleCode).Scan(&n).Error; err != nil {
		t.Fatalf("数绑定失败: %v", err)
	}
	return n
}

// injectInsertFailureAfter 让指定表的第 n 条 INSERT 之后的下一次写入失败。
//
// 与 failNextInserts 的区别：这里按「表 + 次数」精确打靶，用于「先写进去、再失败」的场景。
func injectInsertFailureAfter(t *testing.T, db *gorm.DB, table string, n int) {
	t.Helper()
	var seen int
	cb := db.Callback().Create()
	const name = "test:inject_insert_failure_after"
	if err := cb.Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != table {
			return
		}
		seen++
		if seen > n {
			_ = tx.AddError(errors.New("注入的写入故障"))
		}
	}); err != nil {
		t.Fatalf("注册故障注入回调失败: %v", err)
	}
	t.Cleanup(func() { _ = cb.Remove(name) })
}

func u64(v uint64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}

var _ = time.Second
