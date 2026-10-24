package dbdriver

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// OpenPostgres 创建 PostgreSQL Dialector。
func OpenPostgres(cfg Config) gorm.Dialector {
	return postgres.Open(PostgresDSN(cfg))
}

// PostgresDSN 组装 PostgreSQL 连接串（URL 形式）。
//
// 为什么不用 `host=… user=… password=… dbname=…` 的空白分隔形式：
// 那个格式里**空值会吃掉下一个键**。空密码时得到
//
//	host=h user=u password= dbname=mydb port=5432 …
//
// pgx 把 `dbname=mydb` 当成密码值，解析结果是 pass="dbname=mydb"、db="" ——
// 连库时报的是「库不存在」，排查方向全错。密码或库名含空格 / `@` / `:` / `/`
// 时同样会被拼接错误切断。
//
// URL 形式由 net/url 负责转义，空密码直接省略 userinfo 的密码段
// （`postgres://u@host:5432/mydb?...`，pgx 解析出 db=mydb、pass=""）。
//
// TimeZone 与 statement_timeout（options）都是 pgx 认的运行时参数，放 query 即可：
// options 的值 `-c statement_timeout=5000` 里的空格与等号必须走 `%20`/`%3D` 编码
// （见 encodePostgresQuery）。端到端实测（本机 PG 15）：
//
//	SHOW statement_timeout  => "1500ms"
//	SELECT 1 FROM pg_sleep(3) => ERROR: canceling statement due to statement timeout (57014)
//
// 即 options 真的进了服务端启动参数，而不只是解析层看起来对。
func PostgresDSN(cfg Config) string {
	u := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.DBName,
	}
	// 空密码不写 userinfo 密码段：url.UserPassword("u", "") 产出 "u:@"，pgx 能解析，
	// 但日志与抓包里「一个空密码段」很容易被误读成「配了空字符串密码」。
	if cfg.Password == "" {
		u.User = url.User(cfg.User)
	} else {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}

	// TimeZone 必须落在**末尾且保持字面量**：
	//
	// gorm.io/driver/postgres 的 timeZoneMatcher = `(time_zone|TimeZone|timezone)=(.*?)($|&| )`
	// 直接对 DSN **原文**取值，再拿它调 time.LoadLocation —— 不经过 URL 解码。
	// 所以 `Asia%2FShanghai` 会让任何配置都在 gorm.Open 时死于
	// 「unknown time zone Asia%2FShanghai」（实测）。同理 `Asia+Shanghai` 也不行。
	// 因此这里不用 url.Values.Encode 统一编码，而是自己拼：TimeZone 字面写入、放最后。
	//
	// options 相反：它必须严格转义（空格 %20、等号 %3D），否则 pgx 会报
	// 「extra key/value separator」或把 `+` 当成字面量（见 encodePostgresQuery）。
	q := url.Values{}
	q.Set("sslmode", "disable")
	if cfg.StatementTimeout > 0 {
		q.Set("options", "-c statement_timeout="+strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10))
	}
	u.RawQuery = encodePostgresQuery(q) + "&TimeZone=" + postgresTimeZone
	return u.String()
}

// postgresTimeZone 服务端会话时区（保持字面形式，见 PostgresDSN 里 timeZoneMatcher 的说明）。
const postgresTimeZone = "Asia/Shanghai"

// encodePostgresQuery 把 query 编成 pgx 能正确解读的形式。
//
// 不能直接用 url.Values.Encode()：它按 application/x-www-form-urlencoded 把空格编成 `+`，
// 而 **pgx 不解 `+`**。实测 `options=-c+statement_timeout=5000` 解析出来就是
// `-c+statement_timeout=5000`（字面带加号），交给 Postgres 后是无效的启动参数。
// 必须用 `%20`（ParseQuery 两种都能读回空格，故这里先 Encode 再把 `+` 换回 `%20`）。
//
// 手写未转义的 `-c statement_timeout=5000` 更不行：pgx 会因 query 里多一个 `=` 直接报
// 「extra key/value separator」。
func encodePostgresQuery(q url.Values) string {
	return strings.ReplaceAll(q.Encode(), "+", "%20")
}
