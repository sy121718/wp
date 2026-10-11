package navigationmodel_test

// navigation_model_swap_test.go — SwapSortOrders 的真库行为（X 回滚 + Z1 同快照并发）。
//
// 为什么必须有这一条：这个方法是「同一事务里两条条件 UPDATE」的唯一实现，它的正确性全在
// 几条**不报错、只是结果不同**的语义上：
//
//   - 第二条版本已变 → 第一条必须**一起回滚**（修复前的「半交换」就是这里漏掉的那半：
//     第一条已提交、同级出现重复 sort_order，而调用方只看到一次失败）；
//   - 两行的 update_time 都要刷新（不刷新的话页面拿旧 token 再点一次会静默成功）；
//   - 执行顺序按 id 升序（两笔并发交换不会互相死锁、也不会交叉成功）。
//
// 表结构来自**生产迁移**（support.NewMigratedPGTestDB 复制的模板库），不手抄 CREATE TABLE。
// 列名是 update_time 而不是 updated_at：迁移 205（DB-019）把 navigations.updated_at 改成了
// update_time（与实体 navigation_model.go 的 gorm 映射一致）。
//
// 时间列的可控性：外部干预用 `update_time = update_time + interval '1 second'`，
// 不用 now() —— now() 是事务开始时刻，未必比快照值大（同一微秒内甚至相等），
// 而 +1s 必然产生一个不同的值，用例的「旧 token 必失败」才有确定前提。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/public/test/support"
)

// navSwapRow 一行导航项的持久化读数（排序值与乐观锁 token）。
type navSwapRow struct {
	id      string
	sort    int
	updated time.Time
}

// seedNavSwapRow 插一行顶级菜单项（只补 NOT NULL 列，其余走表默认）。
func seedNavSwapRow(t *testing.T, db *gorm.DB, projectID, title string, sort int) navSwapRow {
	t.Helper()
	row := navSwapRow{id: uuid.NewString(), sort: sort}
	if err := db.Exec(`
		INSERT INTO navigations (id, project_id, title, path, kind, parent_id, sort_order,
			create_time, update_time)
		VALUES (?, ?, ?, ?, 'header', NULL, ?, now(), now())`,
		row.id, projectID, title, "/"+title, sort).Error; err != nil {
		t.Fatalf("准备导航项 %s 失败: %v", title, err)
	}
	return readNavSwapRow(t, db, row.id)
}

// readNavSwapRow 读回一行的 (sort_order, update_time)。
func readNavSwapRow(t *testing.T, db *gorm.DB, id string) navSwapRow {
	t.Helper()
	var row navSwapRow
	if err := db.Raw(
		`SELECT id, sort_order, update_time FROM navigations WHERE id = ?`, id).
		Row().Scan(&row.id, &row.sort, &row.updated); err != nil {
		t.Fatalf("读回导航项 %s 失败: %v", id, err)
	}
	return row
}

// bumpNavSwapRowVersion 外部干预：只改 update_time（+1s），返回干预后的持久化状态。
//
// 这就是「另一个人/另一个窗口改过这一行」的最小形态；用例拿它当**基线**
// （Sol 的 task-84 复验口径：干预提交后记录的状态才是比对照物，
// 不能拿测试初始值去要求「逐字段未变」）。
func bumpNavSwapRowVersion(t *testing.T, db *gorm.DB, id string) navSwapRow {
	t.Helper()
	var row navSwapRow
	if err := db.Raw(`
		UPDATE navigations SET update_time = update_time + interval '1 second'
		WHERE id = ? RETURNING id, sort_order, update_time`, id).
		Row().Scan(&row.id, &row.sort, &row.updated); err != nil {
		t.Fatalf("外部更新导航项 %s 失败: %v", id, err)
	}
	return row
}

// swapPair 把两行按 id 升序摆好：first 是 model 会**先**写的那一行。
//
// 顺序不是随意约定：SwapSortOrders 内部按 id 升序执行（防并发死锁），
// 所以「第二条失败」只能由 id 较大的那一行的版本变化造出来。
func swapPair(a, b navSwapRow) (first, second navSwapRow) {
	if strings.Compare(a.id, b.id) < 0 {
		return a, b
	}
	return b, a
}

// newNavSwapFixture 建一个工程 + 两行数据（真实迁移建表 + 真实父行）。
func newNavSwapFixture(t *testing.T) (*gorm.DB, string, navSwapRow, navSwapRow) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "排序交换工程")
	a := seedNavSwapRow(t, db, projectID, "甲", 1)
	b := seedNavSwapRow(t, db, projectID, "乙", 2)
	return db, projectID, a, b
}

// TestSwapSortOrdersSwapsBothRows：正常路径 —— 两行取值互换、两行的 update_time 都刷新。
func TestSwapSortOrdersSwapsBothRows(t *testing.T) {
	db, projectID, a, b := newNavSwapFixture(t)
	first, second := swapPair(a, b)
	m := navigationmodel.NewModel(db)

	if err := m.SwapSortOrders(context.Background(), projectID,
		navigationmodel.SwapRow{ID: first.id, SortOrder: second.sort, Expected: first.updated},
		navigationmodel.SwapRow{ID: second.id, SortOrder: first.sort, Expected: second.updated},
	); err != nil {
		t.Fatalf("交换应成功，实际: %v", err)
	}
	gotFirst := readNavSwapRow(t, db, first.id)
	gotSecond := readNavSwapRow(t, db, second.id)
	if gotFirst.sort != second.sort || gotSecond.sort != first.sort {
		t.Fatalf("两行取值应互换：first %d→%d（期望 %d）、second %d→%d（期望 %d）",
			first.sort, gotFirst.sort, second.sort, second.sort, gotSecond.sort, first.sort)
	}
	if !gotFirst.updated.After(first.updated) || !gotSecond.updated.After(second.updated) {
		t.Fatalf("两行 update_time 都应刷新：first %v→%v、second %v→%v",
			first.updated, gotFirst.updated, second.updated, gotSecond.updated)
	}
}

// TestSwapSortOrdersRollsBackWhenSecondRowVersionChanged（X 回滚）：
// 第二条（id 较大的那一行）的版本被外部改过 → 第一条**必须一起回滚**。
//
// 这是本卡的主断言：修复前这里是「第一条已提交 + 调用方收到失败」，
// 同级留下两个相同 sort_order（半交换）。基线取「外部干预并提交之后」的持久化状态。
func TestSwapSortOrdersRollsBackWhenSecondRowVersionChanged(t *testing.T) {
	db, projectID, a, b := newNavSwapFixture(t)
	first, second := swapPair(a, b)
	m := navigationmodel.NewModel(db)

	// 外部干预：把**第二条**的版本推走（+1s），并把它当基线。
	baselineSecond := bumpNavSwapRowVersion(t, db, second.id)
	if !baselineSecond.updated.After(second.updated) {
		t.Fatalf("干预应产生更新的 update_time：%v → %v", second.updated, baselineSecond.updated)
	}
	baselineFirst := readNavSwapRow(t, db, first.id)

	err := m.SwapSortOrders(context.Background(), projectID,
		navigationmodel.SwapRow{ID: first.id, SortOrder: second.sort, Expected: first.updated},
		navigationmodel.SwapRow{ID: second.id, SortOrder: first.sort, Expected: second.updated},
	)
	var conflict *navigationmodel.SwapConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("第二条版本已变时应返回 SwapConflictError，实际: %v", err)
	}
	if conflict.ID != second.id {
		t.Fatalf("冲突行应是第二条 %s，实际 %s", second.id, conflict.ID)
	}

	// 基线比对：第一条（在事务里先被写成功的那行）必须回到干预后的原样，
	// 第二条保留外部干预后的版本、排序值不动。
	gotFirst := readNavSwapRow(t, db, first.id)
	gotSecond := readNavSwapRow(t, db, second.id)
	if gotFirst.sort != baselineFirst.sort || !gotFirst.updated.Equal(baselineFirst.updated) {
		t.Fatalf("第一条应完全回滚到基线（sort %d/update %v），实际 sort %d/update %v",
			baselineFirst.sort, baselineFirst.updated, gotFirst.sort, gotFirst.updated)
	}
	if gotSecond.sort != baselineSecond.sort || !gotSecond.updated.Equal(baselineSecond.updated) {
		t.Fatalf("第二条应保持外部干预后的状态（sort %d/update %v），实际 sort %d/update %v",
			baselineSecond.sort, baselineSecond.updated, gotSecond.sort, gotSecond.updated)
	}
	if gotFirst.sort == gotSecond.sort {
		t.Fatalf("同级不得出现重复排序值：first=%d second=%d", gotFirst.sort, gotSecond.sort)
	}
}

// TestSwapSortOrdersConcurrentSameTokensOneWins（Z1）：两笔并发拿着**同一份快照 token**
// 做同一个交换 —— 恰一成功一冲突，终态只可能是「交换后」，不会出现半交换。
//
// 确定性来自 token 本身：胜者刷新了两行的 update_time，败者的条件 UPDATE 必然命中 0 行
// （先写的那行被胜者事务的行锁挡住，等锁结束后 READ COMMITTED 会重读到已提交的新版本）。
func TestSwapSortOrdersConcurrentSameTokensOneWins(t *testing.T) {
	db, projectID, a, b := newNavSwapFixture(t)
	first, second := swapPair(a, b)
	m := navigationmodel.NewModel(db)

	firstRow := navigationmodel.SwapRow{ID: first.id, SortOrder: second.sort, Expected: first.updated}
	secondRow := navigationmodel.SwapRow{ID: second.id, SortOrder: first.sort, Expected: second.updated}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = m.SwapSortOrders(context.Background(), projectID, firstRow, secondRow)
		}(i)
	}
	wg.Wait()

	var ok, conflicts int
	for _, err := range errs {
		var conflict *navigationmodel.SwapConflictError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &conflict):
			conflicts++
		default:
			t.Fatalf("并发交换出现非冲突错误（连接/约束类必须原样上抛）: %v", err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("两笔同快照交换应恰一成功一冲突，实际成功 %d / 冲突 %d（errs=%v）", ok, conflicts, errs)
	}
	gotFirst := readNavSwapRow(t, db, first.id)
	gotSecond := readNavSwapRow(t, db, second.id)
	if gotFirst.sort != second.sort || gotSecond.sort != first.sort {
		t.Fatalf("终态应只有一种合法结果（交换后）：first=%d second=%d", gotFirst.sort, gotSecond.sort)
	}
}

// TestSwapSortOrdersRequiresProjectScope：缺工程作用域直接拒（不能退化成「按 id 直写」）。
func TestSwapSortOrdersRequiresProjectScope(t *testing.T) {
	db, _, a, b := newNavSwapFixture(t)
	first, second := swapPair(a, b)
	m := navigationmodel.NewModel(db)

	err := m.SwapSortOrders(context.Background(), "  ",
		navigationmodel.SwapRow{ID: first.id, SortOrder: second.sort, Expected: first.updated},
		navigationmodel.SwapRow{ID: second.id, SortOrder: first.sort, Expected: second.updated},
	)
	if err == nil {
		t.Fatal("缺工程作用域应报错")
	}
	var conflict *navigationmodel.SwapConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("缺作用域不是乐观锁冲突: %v", err)
	}
	if got := readNavSwapRow(t, db, first.id); got.sort != first.sort {
		t.Fatalf("拒写时不得改动数据：%d → %d", first.sort, got.sort)
	}
}
