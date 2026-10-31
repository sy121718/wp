package support

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPGProbePreservesDSNShapesAndTimeout(t *testing.T) {
	for _, dsn := range []string{
		pgDSN("127.0.0.1", "5432", "root", "a b", "wp_test"),
		"host=127.0.0.1 port=5432 user=root password='a b' dbname=wp_test sslmode=disable TimeZone=Asia/Shanghai synchronous_commit=off",
	} {
		config, err := pgconn.ParseConfig(pgAppendParams(dsn, [2]string{"connect_timeout", "5"}))
		if err != nil {
			t.Fatalf("连接探测配置解析失败: %v", err)
		}
		if config.ConnectTimeout != 5*time.Second || config.Database != "wp_test" || config.Password != "a b" || config.RuntimeParams["synchronous_commit"] != "off" || config.RuntimeParams["TimeZone"] != "Asia/Shanghai" {
			t.Fatalf("连接探测改变了端点、运行参数或未设置超时")
		}
	}
}

func TestPGProbeUsesRealEndpoint(t *testing.T) {
	if err := pingPG(localPGEndpoint()); err != nil {
		FailIfRequiredPG(t, err)
		t.Skipf("本地 PostgreSQL 不可用: %v", err)
	}
}

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
