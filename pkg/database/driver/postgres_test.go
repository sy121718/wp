package dbdriver

// postgres_test.go — 连接串组装。
//
// 守的缺陷：旧实现用 `fmt.Sprintf("host=%s user=%s password=%s dbname=%s …")` 拼
// **空白分隔**的 key=value 串，而那个格式里空值会吃掉下一个键 —— 空密码时得到
//
//	host=h user=u password= dbname=mydb port=5432 …
//
// pgx 把 `dbname=mydb` 当成密码值，解析出 pass="dbname=mydb"、db=""，连库时报
// 「库不存在」，排查方向全错。密码或库名含空格 / @ / : / / 时同样会被切断。
//
// **两条阅读路径都要盯**，这是本文件最重要的一条约定：
//
//  1. pgx.ParseConfig —— 会做 URL 解码。只验它是不够的。
//  2. gorm.io/driver/postgres —— 额外对 DSN **原文**跑正则
//     `(time_zone|TimeZone|timezone)=(.*?)($|&| )`，把值直接交给 time.LoadLocation，
//     **不经过 URL 解码**。所以 TimeZone 一旦被编成 Asia%2FShanghai，任何配置都在
//     gorm.Open 时死于「unknown time zone Asia%2FShanghai」（实测复现过）。
//     这条只有走真实 gorm.Open 才能钤住，见 TestOpenPostgresWorksThroughGorm。

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

// TestPostgresDSNEmptyPasswordIsNotSwallowedByDBName 空密码不得吃掉 dbname。
//
// 这是原缺陷的最小复现：改之前这里 pass="dbname=mydb"、db=""。
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

// TestPostgresDSNPreservesSpecialCharacters 密码/库名里的 @ : / 空格必须原样到达。
//
// 空白分隔形式下这些字符会把连接串切成两半（`p@ss` 里的 @ 会被当成 userinfo 分隔）。
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

// TestPostgresDSNStatementTimeoutAndTimeZone 运行时参数必须进 RuntimeParams。
//
// options 的值自带空格与等号（`-c statement_timeout=5000`）：拼字符串时它会把连接串
// 再切一次，走 query 编码才是安全的。TimeZone 原来就在串里，改成 URL 形式后必须还在。
func TestPostgresDSNStatementTimeoutAndTimeZone(t *testing.T) {
	cases := []struct {
		name    string
		timeout time.Duration
		want    string // options 的期望值；空表示不出现该参数
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
			// pgx 原样保留 `TimeZone` 这个键名（大小写敏感）。值必须是**字面**的
			// Asia/Shanghai —— 编成 Asia%2FShanghai 会让 gorm 的 LoadLocation 直接失败。
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

// TestOpenPostgresWorksThroughGorm 走真实 gorm.Open —— 唯一的权威入口。
//
// 为什么必须有这一条：其余用例都用 pgx.ParseConfig 验字段，而 gorm 驱动在 pgx 解析
// **之外**还做了一件事 —— 对 DSN 原文正则取 TimeZone 再 time.LoadLocation。只验 pgx
// 的话，「TimeZone 被 URL 编码成 Asia%2FShanghai」这种会让**所有部署都启不动**的缺陷
// 能一路绿着过去（本项目真实发生过）。
//
// 数据库连不上时 Skip（本仓口径：PG 不可用则 t.Skip），但只要连上了就必须能 Open 成功。
func TestOpenPostgresWorksThroughGorm(t *testing.T) {
	// 连接参数默认对齐 config.yaml.example，可用标准 libpq 环境变量覆盖。
	//
	// 以前把库名写死成 `artex` —— 那是另一个项目的库，与本仓无关：换一台机器（含 CI）
	// 就没这个库，测试永久 Skip；恰好装了它的机器上，验的也是别人的库。
	cfg := openTestConfig()

	db, err := gorm.Open(OpenPostgres(cfg), &gorm.Config{})
	if err != nil {
		// **Skip 只用于「库不在」**，不能用于「库在但 DSN 组装错了」——
		// 后者是本测试存在的理由（TimeZone 被编码成 Asia%2FShanghai 时 gorm.Open 就在这里
		// 失败，报 unknown time zone）。用「能不能建立最朴素的连接」区分这两件事：
		// 朴素连接也连不上 = 环境没有库，跳过；朴素连接能上、本例却失败 = 组装缺陷，判失败。
		if probeErr := probePostgresReachable(cfg); probeErr != nil {
			t.Skipf("本机没有可用的 PG（跳过）：%v", probeErr)
		}
		t.Fatalf("库连得上，但 OpenPostgres 组装的 DSN 让 gorm.Open 失败 —— 这是真缺陷，不是环境问题：%v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层 *sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	// 两个断言各钉一个维度：会话时区是配置那个（证明 TimeZone 真的生效，不是被丢弃），
	// 且库名解析对了（证明空密码没吃掉 dbname）。
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

// skipOrFailPG 依赖不可用时的口径：宽松模式 Skip，严格模式（GOWP_REQUIRE_PG=1）必须红。
//
// 只写 t.Skipf 时，`GOWP_REQUIRE_PG=1 go test ./...` 会把「库不在」报成 ok，
// 看起来全绿，实际一条真连接都没跑。
func skipOrFailPG(t *testing.T, err error) {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("GOWP_REQUIRE_PG")); v == "1" || strings.EqualFold(v, "true") {
		t.Fatalf("PostgreSQL 不可用，而 GOWP_REQUIRE_PG=1 要求本次测试必须真跑：%v", err)
	}
	t.Skipf("本机没有可用的 PG（跳过）：%v", err)
}

// openTestConfig 真实连接用的 Config：默认对齐 config.yaml.example，
// 可用标准 libpq 环境变量（PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE）覆盖。
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
		Password: os.Getenv("PGPASSWORD"),
		DBName:   envOr("PGDATABASE", "wp"),
	}
}

// probePostgresReachable 用最朴素的连接串探一次库是否可达（不带任何被检参数）。
//
// 只用于把「环境没有库」与「我们组装的串有问题」分开：前者 Skip，后者必须红。
func probePostgresReachable(cfg Config) error {
	probe := "postgres://" + url.User(cfg.User).String() + "@" +
		net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/" + cfg.DBName + "?sslmode=disable"
	conn, err := pgx.Connect(context.Background(), probe)
	if err != nil {
		return err
	}
	return conn.Close(context.Background())
}

// TestPostgresDSNIPv6Host IPv6 字面量 host 必须加方括号，否则端口分隔符无从判断。
//
// 旧实现用空白分隔串（host=::1）时 pgx 能正常解析；改成 URL 形式后若直接
// `cfg.Host + ":" + port` 会产出 postgres://u@::1:5432/db，pgx 报 invalid port。
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
