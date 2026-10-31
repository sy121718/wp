package productmodel_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

func collectionIDsSeed(t *testing.T, db *gorm.DB, projectID string) []string {
	t.Helper()
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
			VALUES (?, ?, ?, ?, ?, 'published', ?, now(), now())`,
			id, projectID, fmt.Sprintf("商品 %d", i+1), fmt.Sprintf("p-ids-%d", i+1),
			fmt.Sprintf("SKU-IDS-%d", i+1), i+1).Error; err != nil {
			t.Fatalf("夹具备料失败：%v", err)
		}
	}
	return ids
}

func captureCollectionVars(t *testing.T, db *gorm.DB, run func() error) (string, []any) {
	t.Helper()
	var sqlText string
	var vars []any
	const hook = "collection_ids:capture"
	if err := db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
		sqlText = tx.Statement.SQL.String()
		vars = append([]any(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}
	defer func() { _ = db.Callback().Query().Remove(hook) }()
	if err := run(); err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	return sqlText, vars
}

func arrayBoundedParam(t *testing.T, sqlText string, vars []any) int {
	t.Helper()
	marker := "= ANY(("
	at := strings.Index(sqlText, marker)
	if at < 0 {
		t.Fatalf("SQL 里没有 = ANY(( 数组谓词：%s", sqlText)
	}
	rest := sqlText[at+len(marker):]
	if !strings.HasPrefix(rest, "$") {
		t.Fatalf("= ANY(( 后面应紧跟数组占位符 $n，实际：%s", sqlText[at:])
	}
	end := 1
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 1 {
		t.Fatalf("= ANY(( 后面不是合法的 $<数字> 占位符：%s", sqlText[at:])
	}
	n, err := strconv.Atoi(rest[1:end])
	if err != nil {
		t.Fatalf("数组占位符编号解析失败（%q）：%v", rest[1:end], err)
	}

	idx := n - 1
	if idx < 0 || idx >= len(vars) {
		t.Fatalf("数组占位符 $%d 对应的参数下标 %d 越界（共 %d 个参数）：%s", n, idx, len(vars), sqlText)
	}
	return idx
}

func pgArrayLiteral(t *testing.T, v any) string {
	t.Helper()
	valuer, ok := v.(driver.Valuer)
	if !ok {
		t.Fatalf("数组占位符绑定的参数应实现 driver.Valuer，实际类型 %T：%v", v, v)
	}
	if rv := reflect.ValueOf(valuer); rv.Kind() == reflect.Ptr && rv.IsNil() {
		t.Fatalf("数组占位符绑定的是 nil 的 driver.Valuer（%T）", v)
	}
	encoded, err := valuer.Value()
	if err != nil {
		t.Fatalf("driver.Valuer.Value() 编码失败：%v", err)
	}
	text, ok := encoded.(string)
	if !ok {
		t.Fatalf("数组字面量编码结果应是 string，实际 %T：%v", encoded, encoded)
	}
	return text
}

func TestCollectionCandidateIDsUseOneArrayParameter(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "集合源候选参数")
	ids := collectionIDsSeed(t, db, projectID)
	m := productmodel.NewModel(db)
	ctx := context.Background()

	t.Run("三个候选只绑一个数组参数且计数正确", func(t *testing.T) {
		var total int64
		sqlText, vars := captureCollectionVars(t, db, func() error {
			var err error
			total, err = m.CountForCollection(ctx, productmodel.CollectionFilter{
				ProjectID: projectID, Status: "published", IDs: ids,
			})
			return err
		})
		if total != 3 {
			t.Fatalf("三个候选应命中三行，实际 %d", total)
		}
		if !strings.Contains(sqlText, "= ANY(") {
			t.Fatalf("候选应走数组参数而不是 IN 列表：%s", sqlText)
		}

		if len(vars) != 3 {
			t.Fatalf("应恰好绑 3 个参数（工程 / 数组 / 状态），实际 %d：%v", len(vars), vars)
		}

		arrayIdx := arrayBoundedParam(t, sqlText, vars)
		literal := pgArrayLiteral(t, vars[arrayIdx])
		for _, id := range ids {
			if !strings.Contains(literal, id) {
				t.Fatalf("数组字面量里缺 id %s：%s", id, literal)
			}
		}

		elements := strings.Split(strings.TrimSuffix(strings.TrimPrefix(literal, "{"), "}"), ",")
		if len(elements) != len(ids) {
			t.Fatalf("数组元素数应与候选数相等（%d），实际 %d：%s", len(ids), len(elements), literal)
		}
		elementSet := make(map[string]bool, len(elements))
		for _, e := range elements {

			unquoted, uerr := strconv.Unquote(e)
			if uerr != nil {
				t.Fatalf("数组元素 %q 不是 Go/PG 双引号包裹的字面量：%v（整体 %s）", e, uerr, literal)
			}
			elementSet[unquoted] = true
		}
		for _, id := range ids {
			if !elementSet[id] {
				t.Fatalf("数组元素集合不含 %s：%s", id, literal)
			}
		}

		for i, v := range vars {
			if i == arrayIdx {
				continue
			}
			text := fmt.Sprint(v)
			for _, id := range ids {
				if strings.Contains(text, id) {
					t.Fatalf("标量参数 %v 里出现了候选 id %s（数组被拆成独立占位符）", v, id)
				}
			}
		}
	})

	t.Run("空集合仍返回零行而不是全量", func(t *testing.T) {
		total, err := m.CountForCollection(ctx, productmodel.CollectionFilter{
			ProjectID: projectID, Status: "published", IDs: []string{},
		})
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if total != 0 {
			t.Fatalf("非 nil 空集合必须等于零行，实际 %d", total)
		}
	})

	t.Run("nil 集合表示不筛，返回全量", func(t *testing.T) {
		total, err := m.CountForCollection(ctx, productmodel.CollectionFilter{
			ProjectID: projectID, Status: "published",
		})
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if total != 3 {
			t.Fatalf("nil 候选不应过滤，实际 %d", total)
		}
	})

	t.Run("取数与计数用同一种参数形状", func(t *testing.T) {
		var rows int
		sqlText, _ := captureCollectionVars(t, db, func() error {
			list, err := m.ListForCollection(ctx, productmodel.CollectionFilter{
				ProjectID: projectID, Status: "published", IDs: ids, Order: "newest",
			}, 20, 0)
			rows = len(list)
			return err
		})
		if rows != 3 {
			t.Fatalf("取数应命中三行，实际 %d", rows)
		}
		if !strings.Contains(sqlText, "= ANY(") {
			t.Fatalf("取数也应走数组参数：%s", sqlText)
		}
	})
}
