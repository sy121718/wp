package dbdriver

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"
)

func TestPostgresDSNEmptyPasswordIsNotSwallowedByDBName(t *testing.T) {
	dsn := PostgresDSN(Config{Host: "127.0.0.1", Port: 5432, User: "u", Password: "", DBName: "mydb"})

	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgx 解析失败（dsn=%q）：%v", dsn, err)
	}
	if parsed.Database != "mydb" {
		t.Errorf("库名解析成了 %q，应为 mydb（dsn=%q）—— 空密码吃掉了 dbname", parsed.Database, dsn)
	}
	if parsed.Password != "" {
		t.Errorf("密码解析成了 %q，应为空（dsn=%q）—— 空密码吃掉了下一个键", parsed.Password, dsn)
	}
	if parsed.User != "u" {
		t.Errorf("用户名解析成了 %q，应为 u", parsed.User)
	}
	if parsed.Host != "127.0.0.1" || parsed.Port != 5432 {
		t.Errorf("地址解析成了 %s:%d，应为 127.0.0.1:5432", parsed.Host, parsed.Port)
	}
}

func TestPostgresDSNPreservesSpecialCharacters(t *testing.T) {
	const (
		password = "p@ss w:rd/xx"
		dbName   = "my db"
	)
	dsn := PostgresDSN(Config{Host: "db.internal", Port: 6543, User: "u", Password: password, DBName: dbName})

	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgx 解析失败（dsn=%q）：%v", dsn, err)
	}
	if parsed.Password != password {
		t.Errorf("密码解析成了 %q，应为 %q —— 特殊字符没被转义", parsed.Password, password)
	}
	if parsed.Database != dbName {
		t.Errorf("库名解析成了 %q，应为 %q", parsed.Database, dbName)
	}
	if parsed.Host != "db.internal" || parsed.Port != 6543 {
		t.Errorf("地址解析成了 %s:%d，应为 db.internal:6543", parsed.Host, parsed.Port)
	}
}

func TestPostgresDSNStatementTimeoutAndTimeZone(t *testing.T) {
	cases := []struct {
		name    string
		timeout time.Duration
		want    string
	}{
		{"未配置超时不带 options", 0, ""},
		{"默认超时", 5 * time.Second, "-c statement_timeout=5000"},
		{"亚毫秒向下取整", 500 * time.Microsecond, "-c statement_timeout=0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dsn := PostgresDSN(Config{
				Host: "127.0.0.1", Port: 5432, User: "u", DBName: "mydb",
				StatementTimeout: tc.timeout,
			})
			parsed, err := pgx.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("pgx 解析失败（dsn=%q）：%v", dsn, err)
			}

			if got := parsed.RuntimeParams["TimeZone"]; got != "Asia/Shanghai" {
				t.Errorf("TimeZone 解析成了 %q，应为 Asia/Shanghai（dsn=%q）", got, dsn)
			}
			got, ok := parsed.RuntimeParams["options"]
			if tc.want == "" {
				if ok {
					t.Errorf("未配置超时时不该带 options，得到 %q", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("options 解析成了 %q，应为 %q（dsn=%q）", got, tc.want, dsn)
			}
		})
	}
}

func TestOpenPostgresWorksThroughGorm(t *testing.T) {

	cfg := openTestConfig()

	db, err := gorm.Open(OpenPostgres(cfg), &gorm.Config{})
	if err != nil {

		if probeErr := probePostgresReachable(cfg); probeErr != nil {
			skipOrFailPG(t, probeErr)
		}
		t.Fatalf("库连得上，但 OpenPostgres 组装的 DSN 让 gorm.Open 失败 —— 这是真缺陷，不是环境问题：%v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层 *sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	var tz, dbName string
	if err = db.Raw("SHOW TimeZone").Scan(&tz).Error; err != nil {
		t.Fatalf("SHOW TimeZone 失败: %v", err)
	}
	if tz != "Asia/Shanghai" {
		t.Errorf("会话时区是 %q，应为 Asia/Shanghai —— TimeZone 没生效（被 URL 编码后 gorm 的 LoadLocation 会直接失败）", tz)
	}
	if err = db.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil {
		t.Fatalf("查询当前库名失败: %v", err)
	}
	if dbName != cfg.DBName {
		t.Errorf("连到了库 %q，应为 %q —— 空密码吃掉了 dbname", dbName, cfg.DBName)
	}
}

func skipOrFailPG(t *testing.T, err error) {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("GOWP_REQUIRE_PG")); v == "1" || strings.EqualFold(v, "true") {
		t.Fatalf("PostgreSQL 不可用，而 GOWP_REQUIRE_PG=1 要求本次测试必须真跑：%v", err)
	}
	t.Skipf("本机没有可用的 PG（跳过）：%v", err)
}

func openTestConfig() Config {
	envOr := func(key, fallback string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	port, err := strconv.Atoi(envOr("PGPORT", "5432"))
	if err != nil {
		panic("PGPORT 不是数字: " + os.Getenv("PGPORT"))
	}
	return Config{
		Host:     envOr("PGHOST", "127.0.0.1"),
		Port:     port,
		User:     envOr("PGUSER", "root"),
		Password: envOr("PGPASSWORD", "root"),
		DBName:   envOr("PGDATABASE", "wp"),
	}
}

func probePostgresReachable(cfg Config) error {
	conn, err := pgx.Connect(context.Background(), probePostgresDSN(cfg))
	if err != nil {
		return err
	}
	return conn.Close(context.Background())
}

func probePostgresDSN(cfg Config) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.DBName,
	}
	query := u.Query()
	query.Set("sslmode", "disable")
	u.RawQuery = query.Encode()
	return u.String()
}

func TestPostgresDSNIPv6Host(t *testing.T) {
	dsn := PostgresDSN(Config{Host: "::1", Port: 5432, User: "u", DBName: "mydb"})
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("IPv6 host 解析失败（dsn=%q）：%v", dsn, err)
	}
	if parsed.Host != "::1" || parsed.Port != 5432 {
		t.Errorf("地址解析成了 %s:%d，应为 [::1]:5432（dsn=%q）", parsed.Host, parsed.Port, dsn)
	}
}

func TestProbePostgresDSNKeepsIdentity(t *testing.T) {
	const password = "p@ss:w/rd#x"
	cfg := Config{Host: "127.0.0.1", Port: 5432, User: "probe", Password: password, DBName: "probe"}

	parsed, err := pgx.ParseConfig(probePostgresDSN(cfg))
	if err != nil {
		t.Fatalf("探针串无法解析（口令里的特殊字符把 URL 切坏了）: %v", err)
	}
	if parsed.Host != cfg.Host || parsed.Port != uint16(cfg.Port) {
		t.Errorf("地址解析成 %s:%d，应为 %s:%d", parsed.Host, parsed.Port, cfg.Host, cfg.Port)
	}
	if parsed.User != cfg.User {
		t.Errorf("用户解析成 %q，应为 %q", parsed.User, cfg.User)
	}
	if parsed.Password != password {
		t.Errorf("口令解析成 %q，应为 %q —— 探针丢了口令，「库在、口令错」会被当成「库不在」而 Skip", parsed.Password, password)
	}
	if parsed.Database != cfg.DBName {
		t.Errorf("库名解析成 %q，应为 %q", parsed.Database, cfg.DBName)
	}
}
