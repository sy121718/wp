package dbdriver

// mysql_test.go — MySQL 连接串组装。
//
// 守的缺陷（**实测确认的那些，不是推测的**）：旧实现用
//
//	fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local", …)
//
// 拼串，库名里出现 `/` 或 `?` 时出事：
//
//	库名 db/x          => ParseDSN 直接失败: invalid DSN: did you forget to escape a param value?
//	库名 my?db         => 静默截断成 my，后面的串被当成 DSN 参数
//	库名 db?charset=latin1 => 截断 + **改写查询参数**（charset 被覆盖）
//
// 反过来，密码里的 `@` / `:` / `/` 旧拼法**是安全的** —— go-sql-driver 从右往左
// 找 `@tcp(` 定位 userinfo，所以不必假装它坏了。这也说明「凭想象报缺陷」很容易
// 报错方向：真正脆弱的是 **库名** 与 query 段，不是密码。
//
// 现在交给 go-sql-driver 自己的 Config.FormatDSN（它才是解析方，转义规则不可能漂）。
// 断言用 ParseDSN 往返核对字段，而不是比对 DSN 字面量 —— 字面量随驱动升级会变，
// 「往返后字段一致」才是要保证的性质。

import (
	"strconv"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// TestMySQLDSNRoundTripsAwkwardNames 各种别扭的库名/用户名/密码经往返不丢、不被截断。
func TestMySQLDSNRoundTripsAwkwardNames(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"空密码", Config{Host: "127.0.0.1", Port: 3306, User: "u", Password: "", DBName: "mydb"}},
		{"密码含 @ : /", Config{Host: "127.0.0.1", Port: 3306, User: "u", Password: "p@ss/wo:rd", DBName: "mydb"}},
		{"库名含空格", Config{Host: "db.internal", Port: 3307, User: "app", Password: "s3cret", DBName: "my db"}},
		// 下面三个是旧拼法真正出事的形态（解析失败 / 静默截断 / 参数被注入）。
		{"库名含斜杠", Config{Host: "127.0.0.1", Port: 3306, User: "u", Password: "p", DBName: "db/x"}},
		{"库名含问号", Config{Host: "127.0.0.1", Port: 3306, User: "u", Password: "p", DBName: "my?db"}},
		{"库名像 query 参数", Config{Host: "127.0.0.1", Port: 3306, User: "u", Password: "p", DBName: "db?charset=latin1"}},
		{"用户名含 @", Config{Host: "127.0.0.1", Port: 3306, User: "u@corp", Password: "p", DBName: "mydb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dsn := MySQLDSNConfig(tc.cfg).FormatDSN()
			back, err := mysqldriver.ParseDSN(dsn)
			if err != nil {
				t.Fatalf("解析自己产出的 DSN 失败（dsn=%q）：%v", dsn, err)
			}
			if back.User != tc.cfg.User {
				t.Errorf("用户名往返后是 %q，应为 %q（dsn=%q）", back.User, tc.cfg.User, dsn)
			}
			if back.Passwd != tc.cfg.Password {
				t.Errorf("密码往返后是 %q，应为 %q（dsn=%q）", back.Passwd, tc.cfg.Password, dsn)
			}
			if back.DBName != tc.cfg.DBName {
				t.Errorf("库名往返后是 %q，应为 %q（dsn=%q）—— 被截断或未转义", back.DBName, tc.cfg.DBName, dsn)
			}
			wantAddr := tc.cfg.Host + ":" + strconv.Itoa(tc.cfg.Port)
			if back.Addr != wantAddr {
				t.Errorf("地址往返后是 %q，应为 %q（dsn=%q）", back.Addr, wantAddr, dsn)
			}
			// 库名里的 `?` 不能变成 query 参数（旧拼法在这里会被改写 charset）。
			//
			// 断言 DSN 文本而不是回读 Config.Params：新版驱动的 ParseDSN 把 charset 存进
			// 私有字段 charsets，不回填 Params，回读恒为空（与实现无关的假失败）。
			// 判据：query 段里 charset 必须仍是 utf8mb4。用 ParseDSN 回读 Params 不可行
			// （新版驱动把 charset 存进私有 charsets、不回填 Params），所以直接查 DSN 文本。
			if !strings.Contains(dsn, "charset=utf8mb4") {
				t.Errorf("DSN 里没有 charset=utf8mb4 —— 库名里的 ? 把后面的串解释成了参数（dsn=%q）", dsn)
			}
		})
	}
}

// TestMySQLDSNKeepsDriverOptions gorm 依赖的驱动选项不能被组装过程丢掉。
//
// parseTime 决定 DATETIME 扫成 time.Time（关掉会让 model 的时间字段扫描失败）；
// loc 是时区口径，与 PG 侧的 TimeZone 对齐。
func TestMySQLDSNKeepsDriverOptions(t *testing.T) {
	cfg := MySQLDSNConfig(Config{Host: "127.0.0.1", Port: 3306, User: "u", DBName: "mydb"})
	if !cfg.ParseTime {
		t.Error("ParseTime 必须为 true（否则 DATETIME 扫不成 time.Time）")
	}
	if cfg.Loc == nil {
		t.Error("Loc 不能为空（否则时间按 UTC 解读，与 PG 侧的 Asia/Shanghai 口径不一致）")
	}
	// charset 存私有字段（charsets），只能从 FormatDSN 的产物看。
	if dsn := cfg.FormatDSN(); !strings.Contains(dsn, "charset=utf8mb4") {
		t.Errorf("FormatDSN 没带 charset=utf8mb4（dsn=%q）", dsn)
	}
}
