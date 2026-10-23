package dbdriver

// dsn_test.go — 连接串构造的**字段级**契约。
//
// 这里不猜字符串长什么样，而是把构造出来的 DSN 交给驱动自己的解析器，再断言解析出的
// 字段等于输入配置。这样「换一种拼法」（URL → 关键字串 → Config 结构体）不会让测试
// 变成维护负担，而「拼错了」一定会红。
//
// 被守的历史事故（pkg/database/driver/postgres.go 的注释里有完整叙述）：
//   - 空密码：`password= dbname=wp` → password="dbname=wp"、database=""，
//     连接失败时报的是 `database "root" does not exist`，与真因无关；
//   - 含空格的密码：关键字串直接解析失败；
//   - 含 @ / : / / 的密码：用户信息段错位（MySQL 侧同样）。

import (
	"regexp"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// decodePG 用 pgx 的解析器把 DSN 还原成字段（drivers 自己的口径，不是我们的）。
func decodePG(t *testing.T, dsn string) *pgconn.Config {
	t.Helper()
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("构造出的 DSN 驱动自己解析不了：%v\nDSN=%s", err, dsn)
	}
	return cfg
}

func basePG() Config {
	return Config{Host: "127.0.0.1", Port: 5432, User: "root", DBName: "wp"}
}

// TestPostgresDSNPasswordShapes 密码的各种形态都必须无损落到 password 字段，
// 且不得吃掉 dbname —— 这是本文件存在的首要理由。
func TestPostgresDSNPasswordShapes(t *testing.T) {
	for _, tc := range []struct{ name, password string }{
		{"空密码", ""},
		{"含空格", "a b c"},
		{"含引号", `a'b"c`},
		{"含 @ : /", "p@ss:w/d"},
		{"含 # 与 ?", "p#ss?wd"},
		{"纯数字", "123456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := basePG()
			cfg.Password = tc.password
			got := decodePG(t, PostgresDSN(cfg))
			// 等式断言：密码原样、数据库名不被吃掉。
			if got.Password != tc.password {
				t.Errorf("password 应为 %q，得到 %q（DSN=%s）", tc.password, got.Password, PostgresDSN(cfg))
			}
			if got.Database != cfg.DBName {
				t.Errorf("database 应为 %q，得到 %q —— 密码把后面的值吃掉了", cfg.DBName, got.Database)
			}
			if got.User != cfg.User {
				t.Errorf("user 应为 %q，得到 %q", cfg.User, got.User)
			}
		})
	}
}

// TestPostgresDSNEmptyPasswordKeepsDatabase 空密码这条单独再钉一次：
// 它是报告里那次「报错指向错误数据库」的现场。
func TestPostgresDSNEmptyPasswordKeepsDatabase(t *testing.T) {
	cfg := basePG() // Password 为零值（空串）
	got := decodePG(t, PostgresDSN(cfg))
	if got.Password != "" {
		t.Errorf("空密码应解析成空串，得到 %q", got.Password)
	}
	if got.Database != "wp" {
		t.Fatalf("空密码吃掉了 dbname：database=%q（历史事故：报错说 database \"root\" does not exist）", got.Database)
	}
}

// TestPostgresDSNHostPortAndSettings host/port 与两个固定参数一并核对：
// 换 DSN 形式时最容易丢的就是这几个「不在结构体里显式出现」的值。
func TestPostgresDSNHostPortAndSettings(t *testing.T) {
	cfg := basePG()
	cfg.Host = "db.internal"
	cfg.Port = 6543
	cfg.DBName = "wp_prod"
	got := decodePG(t, PostgresDSN(cfg))
	if got.Host != "db.internal" || got.Port != 6543 {
		t.Errorf("host/port 应为 db.internal:6543，得到 %s:%d", got.Host, got.Port)
	}
	if got.Database != "wp_prod" {
		t.Errorf("database 应为 wp_prod，得到 %q", got.Database)
	}
	// sslmode=disable 在 pgx 里表现为 TLSConfig == nil（不走 TLS）。
	if got.TLSConfig != nil {
		t.Error("sslmode=disable 应解析成不走 TLS，得到非 nil 的 TLSConfig")
	}
	if tz := got.RuntimeParams["TimeZone"]; tz != "Asia/Shanghai" {
		t.Errorf("TimeZone 应为 Asia/Shanghai，得到 %q", tz)
	}
}

// TestPostgresDSNStatementTimeout statement_timeout 走 options，值必须原样到服务端。
func TestPostgresDSNStatementTimeout(t *testing.T) {
	cfg := basePG()
	cfg.StatementTimeout = 5 * time.Second
	got := decodePG(t, PostgresDSN(cfg))
	if opt := got.RuntimeParams["options"]; opt != "-c statement_timeout=5000" {
		t.Errorf("options 应为 \"-c statement_timeout=5000\"，得到 %q", opt)
	}
	// 未设置时不输出该参数（空 options 是合法的，但会多一次无意义的服务端解析）。
	zero := decodePG(t, PostgresDSN(basePG()))
	if opt, ok := zero.RuntimeParams["options"]; ok {
		t.Errorf("未设置 StatementTimeout 时不该带 options，得到 %q", opt)
	}
}

// TestMySQLDSNPasswordShapes MySQL 侧同一个判据：用户信息段里的分隔符不得错位。
func TestMySQLDSNPasswordShapes(t *testing.T) {
	for _, tc := range []struct{ name, password string }{
		{"空密码", ""},
		{"含 @", "p@ss"},
		{"含 : 与 /", "p:ss/wd"},
		{"含空格与引号", `a b'c"d`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Host: "127.0.0.1", Port: 3306, User: "root", DBName: "wp", Password: tc.password}
			got, err := gomysql.ParseDSN(MySQLDSN(cfg))
			if err != nil {
				t.Fatalf("构造出的 DSN 驱动自己解析不了：%v\nDSN=%s", err, MySQLDSN(cfg))
			}
			if got.Passwd != tc.password {
				t.Errorf("password 应为 %q，得到 %q（DSN=%s）", tc.password, got.Passwd, MySQLDSN(cfg))
			}
			if got.DBName != "wp" {
				t.Errorf("dbname 应为 wp，得到 %q —— 密码把后面的值吃掉了", got.DBName)
			}
			if got.User != "root" {
				t.Errorf("user 应为 root，得到 %q", got.User)
			}
			if got.Addr != "127.0.0.1:3306" {
				t.Errorf("addr 应为 127.0.0.1:3306，得到 %q", got.Addr)
			}
			if !got.ParseTime {
				t.Error("ParseTime 应为 true（time.Time 列的直接映射依赖它）")
			}
			if !strings.Contains(MySQLDSN(cfg), "charset=utf8mb4") {
				t.Errorf("DSN 里应带 charset=utf8mb4，得到 %q", MySQLDSN(cfg))
			}
			// 回环等值：解析回来再拼一遍必须逐字节相同。go-sql-driver 把 charset 放在
			// 未导出的 charsets 字段里（解析后 Params 是空的），回环比直接看字段更稳。
			if round := got.FormatDSN(); round != MySQLDSN(cfg) {
				t.Errorf("DSN 回环不等值：\n  构造 %q\n  回环 %q", MySQLDSN(cfg), round)
			}
		})
	}
}

// TestPostgresDSNTimeZoneSurvivesGorm 守住一个**只在真实启动时才炸**的坑。
//
// gorm 的 postgres 驱动不把整个 DSN 交给 pgx 就完事，它还会用正则从 DSN 原文里抠出
// `TimeZone=` 之后的值，递给 time.LoadLocation（用来给 timestamp 列设 ScanLocation）。
// 因此 DSN 里时区名**不能被百分号转义**：`Asia%2FShanghai` 在 pgx 眼里是合法值，
// 在 time.LoadLocation 眼里是一个不存在的时区，启动直接失败：
//
//	数据库初始化失败: 连接数据库失败: unknown time zone Asia%2FShanghai
//
// 这条测试就是那次真实故障的最小复现 —— 它不连库，只复刻 gorm 那段正则 + 加载时区。
func TestPostgresDSNTimeZoneSurvivesGorm(t *testing.T) {
	// 与 gorm.io/driver/postgres 的 timeZoneMatcher 逐字一致（同包不可导出，故复刻）。
	re := regexp.MustCompile("(time_zone|TimeZone|timezone)=(.*?)($|&| )")
	m := re.FindStringSubmatch(PostgresDSN(basePG()))
	if len(m) <= 2 {
		t.Fatalf("DSN 里找不到 TimeZone 参数，gorm 会给 timestamp 列用错时区：%s", PostgresDSN(basePG()))
	}
	if _, err := time.LoadLocation(m[2]); err != nil {
		t.Fatalf("gorm 从 DSN 抠出的时区 %q 加载失败：%v\nDSN=%s\n"+
			"（启动时表现：数据库初始化失败: unknown time zone）", m[2], err, PostgresDSN(basePG()))
	}
	if m[2] != "Asia/Shanghai" {
		t.Errorf("时区应为 Asia/Shanghai，得到 %q", m[2])
	}
}
