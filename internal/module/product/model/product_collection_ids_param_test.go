package productmodel_test

// product_collection_ids_param_test.go — 集合源候选 id 的**参数形状**判据（PRD-007）。
//
// 公开目录把「本工程已上线实体全集」当候选塞进集合源过滤：十万级商品时，若按 IN (?,?,…)
// 发送就是十万个绑定参数，PostgreSQL 扩展协议上限 65535 —— 请求直接 500。
// 现在改成一个数组参数（products.id = ANY((?::text[])::uuid[])），语义不变、参数个数为 1。
//
// 本用例钉住三件事：参数个数、语义等价（空集合仍返回零行、nil 仍表示不筛）、
// 以及**真的能跑**（数组字面量编码错的话 PG 会报 22P02 / 42883，而不是悄悄返回零行）。

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

// collectionIDsSeed 灌 3 个已上架商品，返回它们的 id（顺序固定，便于比对字面量）。
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

// captureCollectionVars 跑一次查询并返回 SQL 与绑定参数快照。
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

// arrayBoundedParam 按 SQL 里的数组占位符定位那个绑定参数。
//
// 不写死「最后一个参数」：占位符顺序由 collectionQuery 逐条追加条件决定
// （工程 → 候选数组 → 状态 → …），末位是不是数组取决于今天还有几个维度。
//
// 定位读的是**生成的 SQL 本身**：PostgreSQL 的占位符是 $1 / $2 …（不是 ?），
// Statement.SQL 里看到的永远是编号形态，数 "?" 一个都数不到（下标会变成 -1）。
// 定位到 marker 之后，从那里起取第一个 $<数字>，编号减 1 就是 Statement.Vars 的下标
// —— 切片起点落在 marker 末尾，正好跳过「数组自己」，不会把它多算成前面那个。
//
// 返回**下标**而不是值：调用方还要用它做「标量反证」，而 Vars 里躺着的是接口值 ——
// 数组那个位置的动态类型是 driver.Valuer（textArrayParam，见 product_bulk_model.go），
// 拿接口做 == 比较会在动态类型为 slice 时 panic，也不好按值跳过。用下标跳过最稳。
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
	// PG 占位符从 $1 起；Statement.Vars 是从 0 起的同一个序列。
	idx := n - 1
	if idx < 0 || idx >= len(vars) {
		t.Fatalf("数组占位符 $%d 对应的参数下标 %d 越界（共 %d 个参数）：%s", n, idx, len(vars), sqlText)
	}
	return idx
}

// pgArrayLiteral 把绑定参数取成**真正发给 PG 的文本**。
//
// Statement.Vars 里保留的是 driver.Valuer 对象（textArrayParam 是个 []string 的命名类型，
// 只有 Value() 方法产出 {"a","b"} 这种字面量）。fmt.Sprint 打印的是这个 Go 值本身
// ——结果是 [a b c]，看起来像 PG 数组但根本不是要验证的编码。所以这里显式调用
// Value()，检查 err 后再看文本。
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
		// 绑定参数：project_id + 数组 + status —— 共 3 个，且数组元素是内联的
		// （数组字面量文本里能看到三个 id），元素个数不再等于参数个数。
		if len(vars) != 3 {
			t.Fatalf("应恰好绑 3 个参数（工程 / 数组 / 状态），实际 %d：%v", len(vars), vars)
		}
		// 参数顺序是 SQL 里逐个占位符的**追加**顺序（工程 → 候选数组 → 状态），
		// 不是「数组一定在最后」：断言按 SQL 里的数组占位符定位，换维度或调整条件顺序
		// 都不会让这条断言悄悄盯错参数。
		arrayIdx := arrayBoundedParam(t, sqlText, vars)
		literal := pgArrayLiteral(t, vars[arrayIdx])
		for _, id := range ids {
			if !strings.Contains(literal, id) {
				t.Fatalf("数组字面量里缺 id %s：%s", id, literal)
			}
		}
		// 元素集合**精确匹配**：三个 id 都在、且数组里不多不少正是这三个 ——
		// 只查 Contains 会放过「多带了一个 id」这种更危险的形状。
		elements := strings.Split(strings.TrimSuffix(strings.TrimPrefix(literal, "{"), "}"), ",")
		if len(elements) != len(ids) {
			t.Fatalf("数组元素数应与候选数相等（%d），实际 %d：%s", len(ids), len(elements), literal)
		}
		elementSet := make(map[string]bool, len(elements))
		for _, e := range elements {
			// 编码用 strconv.Quote（见 encodePGTextArray），这里按同一条规则去掉包裹引号。
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
		// 反证：标量参数（工程 / 状态）里一个 id 都不该出现 —— 出现说明数组元素被拆回了
		// 独立占位符，参数个数只是碰巧对上。
		//
		// 跳过数组那个位置用**下标**：Vars 里是接口值，数组位置的动态类型是 slice
		// （textArrayParam），拿接口做 == 比较会 panic。
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
