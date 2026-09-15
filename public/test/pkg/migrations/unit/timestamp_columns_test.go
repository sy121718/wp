package unit

// timestamp_columns_test.go — 时间列类型统一（审计 DB-018）的护栏。
//
// 迁移 172 把全库 93 列 timestamp without time zone 统一成 timestamptz。
// 但「改完这一次」不等于「以后也对」：新增一张表、有人照抄旧 DDL 写 TIMESTAMP(3)，
// 无时区列就会悄悄回来，而它带来的问题（跨表比较、换服务器时区后解读不一致）
// 不会当场报错，只会在某次部署后表现为「时间对不上」。
//
// 因此这条测试盯的是**迁移之后的全库状态**，而不是某一条迁移的内容：
// 只要还有无时区的时间列，它就红。

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"
)

// timestampColumn 一个无时区时间列。
type timestampColumn struct {
	TableName  string `gorm:"column:table_name"`
	ColumnName string `gorm:"column:column_name"`
}

// TestNoTimestampWithoutTimeZoneColumns 迁移之后全库不应再有无时区时间列。
func TestNoTimestampWithoutTimeZoneColumns(t *testing.T) {
	db := newMigrationDB(t)
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
	}
	assertNoNaiveTimestamps(t, db)
}

// TestMigration172ConvertsNaiveTimestamps 迁移 172 确实把无时区列改成了 timestamptz。
//
// 上面那条测的是终态；这条测的是「改造动作本身有效」—— 先造一张带无时区列的表，
// 再单独执行 172 的 SQL，断言列类型已变、且**历史值按会话时区解释**（时刻不变）。
func TestMigration172ConvertsNaiveTimestamps(t *testing.T) {
	db := newMigrationDB(t)
	sqlText := migrationSQL(t, "172-time-columns-to-timestamptz")

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS tz_probe (
		id bigserial PRIMARY KEY,
		create_time timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("建探针表失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP TABLE IF EXISTS tz_probe").Error })

	// 写入一个「本地墙钟」值：与种子/迁移里 now() 的写法一致。
	if err := db.Exec(`INSERT INTO tz_probe (create_time) VALUES (TIMESTAMP '2026-01-02 10:00:00')`).Error; err != nil {
		t.Fatalf("写入探针数据失败: %v", err)
	}

	if err := db.Exec(sqlText).Error; err != nil {
		t.Fatalf("执行迁移 172 失败: %v", err)
	}

	var dtype string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'tz_probe' AND column_name = 'create_time'`).
		Scan(&dtype).Error; err != nil {
		t.Fatalf("查询列类型失败: %v", err)
	}
	if dtype != "timestamp with time zone" {
		t.Fatalf("迁移后应为 timestamp with time zone，实际 %q", dtype)
	}

	// 历史值的解释：本地墙钟 10:00 应被还原为「当时那个会话时区下的 10:00」，
	// 因此与显式按同一时区构造的时刻相等 —— 时刻本身没有偏移。
	var roundTrip bool
	if err := db.Raw(`SELECT create_time = (TIMESTAMP '2026-01-02 10:00:00' AT TIME ZONE current_setting('TimeZone'))
		FROM tz_probe`).Scan(&roundTrip).Error; err != nil {
		t.Fatalf("核对历史值失败: %v", err)
	}
	if !roundTrip {
		t.Fatal("历史值应被按会话时区解释为同一时刻（时间点不得偏移）")
	}
}

// assertNoNaiveTimestamps 断言当前 schema 里没有无时区时间列。
func assertNoNaiveTimestamps(t *testing.T, db *gorm.DB) {
	t.Helper()
	var cols []timestampColumn
	if err := db.Raw(`SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND data_type = 'timestamp without time zone'
		ORDER BY table_name, column_name`).Scan(&cols).Error; err != nil {
		t.Fatalf("查询时间列失败: %v", err)
	}
	if len(cols) == 0 {
		return
	}
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.TableName+"."+c.ColumnName)
	}
	t.Fatalf("发现 %d 个无时区时间列（审计 DB-018 要求全部为 timestamptz）：%s",
		len(cols), strings.Join(names, ", "))
}

// migrationSQL 取指定版本迁移的 SQL 文本。
func migrationSQL(t *testing.T, version string) string {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == version {
			return m.SQL
		}
	}
	t.Fatalf("迁移 %s 未注册", version)
	return ""
}
