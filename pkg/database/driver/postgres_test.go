package dbdriver

// postgres_test.go — 连接串组装。
//
// 守的缺陷：旧实现用 `fmt.Sprintf("host=%s user=%s password=%s dbname=%s …")` 拼
// **空白分隔**的 key=value 串，而那个格式里空值会吃掉下一个键 —— 空密码时得到
//
//	host=h user=u password= dbname=mydb port=5432 …
//
// pgx 把 `dbname=mydb` 当成密码值，解析出 pass="dbname=mydb"、db=""，连库时报
// 「库不存在」，排查方向全错。这里不空口断言「改成 URL 就没问题」：每个用例都把
// 组装结果交给 pgx.ParseConfig（与 gorm postgres 驱动同一条解析路径）再核对字段。

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
