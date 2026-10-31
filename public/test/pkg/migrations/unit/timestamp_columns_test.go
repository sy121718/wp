package unit

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"
)

type timestampColumn struct {
	TableName  string `gorm:"column:table_name"`
	ColumnName string `gorm:"column:column_name"`
}

func TestNoTimestampWithoutTimeZoneColumns(t *testing.T) {
	db := newMigrationDB(t)
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
	}
	assertNoNaiveTimestamps(t, db)
}

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

	var roundTrip bool
	if err := db.Raw(`SELECT create_time = (TIMESTAMP '2026-01-02 10:00:00' AT TIME ZONE current_setting('TimeZone'))
		FROM tz_probe`).Scan(&roundTrip).Error; err != nil {
		t.Fatalf("核对历史值失败: %v", err)
	}
	if !roundTrip {
		t.Fatal("历史值应被按会话时区解释为同一时刻（时间点不得偏移）")
	}
}

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
