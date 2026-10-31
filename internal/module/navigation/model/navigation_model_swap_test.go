package navigationmodel_test

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

type navSwapRow struct {
	id      string
	sort    int
	updated time.Time
}

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

func swapPair(a, b navSwapRow) (first, second navSwapRow) {
	if strings.Compare(a.id, b.id) < 0 {
		return a, b
	}
	return b, a
}

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

func TestSwapSortOrdersRollsBackWhenSecondRowVersionChanged(t *testing.T) {
	db, projectID, a, b := newNavSwapFixture(t)
	first, second := swapPair(a, b)
	m := navigationmodel.NewModel(db)

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
