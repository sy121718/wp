package support

// pgdsn_test.go — 测试库 DSN 的字段级契约。
//
// 与 pkg/database/driver/dsn_test.go 同一个判据、同一个理由：测试基建此前自己拼
// 关键字格式的 DSN，空密码时 `password= dbname=x` 被 libpq 解析成
// password="dbname=x"、database="" —— 连不上时报的是 `database "root" does not exist`，
// 与真因毫无关系。
//
// 这条缺陷在应用侧修过（dbdriver.PostgresDSN），测试侧却是同一个坑的另一条分支，
// 一直靠 POSTGRES_PASSWORD=root / DefaultPGPassword="root" 掩盖着。现在两者共用同一份
// 构造，这里钉住「测试侧也真的接上了」。

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestPgDSNPasswordShapes 各种密码形态都必须无损落到 password 字段，且不吃掉 dbname。
func TestPgDSNPasswordShapes(t *testing.T) {
	for _, tc := range []struct{ name, password string }{
		{"空密码", ""},
		{"普通", "root"},
		{"含空格", "a b c"},
		{"含引号", `a'b"c`},
		{"含 @ : /", "p@ss:w/d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn := pgDSN("127.0.0.1", "5432", "root", tc.password, "wp_test")
			cfg, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("构造出的 DSN 驱动自己解析不了: %v\nDSN=%s", err, dsn)
			}
			if cfg.Password != tc.password {
				t.Errorf("password 应为 %q，得到 %q", tc.password, cfg.Password)
			}
			if cfg.Database != "wp_test" {
				t.Errorf("dbname 被密码吃掉了：database=%q（历史事故：报错说 database \"root\" does not exist）", cfg.Database)
			}
			if cfg.User != "root" {
				t.Errorf("user 应为 root，得到 %q", cfg.User)
			}
		})
	}
}

// TestPgDSNKeepsTestSpecificParams 测试专用参数不能丢（synchronous_commit=off 是性能前提）。
func TestPgDSNKeepsTestSpecificParams(t *testing.T) {
	dsn := pgDSN("127.0.0.1", "5432", "root", "", "wp_test")
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := cfg.RuntimeParams["synchronous_commit"]; got != "off" {
		t.Errorf("synchronous_commit 应为 off（全量测试 227s→181s 的前提），得到 %q", got)
	}
	if got := cfg.RuntimeParams["TimeZone"]; got != "Asia/Shanghai" {
		t.Errorf("TimeZone 应为 Asia/Shanghai，得到 %q", got)
	}
}
