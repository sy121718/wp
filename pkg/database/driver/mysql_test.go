package dbdriver

import (
	"strconv"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestMySQLDSNRoundTripsAwkwardNames(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"空密码", Config{Host: "127.0.0.1", Port: 3306, User: "u", Password: "", DBName: "mydb"}},
		{"密码含 @ : /", Config{Host: "127.0.0.1", Port: 3306, User: "u", Password: "p@ss/wo:rd", DBName: "mydb"}},
		{"库名含空格", Config{Host: "db.internal", Port: 3307, User: "app", Password: "s3cret", DBName: "my db"}},

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

			if !strings.Contains(dsn, "charset=utf8mb4") {
				t.Errorf("DSN 里没有 charset=utf8mb4 —— 库名里的 ? 把后面的串解释成了参数（dsn=%q）", dsn)
			}
		})
	}
}

func TestMySQLDSNKeepsDriverOptions(t *testing.T) {
	cfg := MySQLDSNConfig(Config{Host: "127.0.0.1", Port: 3306, User: "u", DBName: "mydb"})
	if !cfg.ParseTime {
		t.Error("ParseTime 必须为 true（否则 DATETIME 扫不成 time.Time）")
	}
	if cfg.Loc == nil {
		t.Error("Loc 不能为空（否则时间按 UTC 解读，与 PG 侧的 Asia/Shanghai 口径不一致）")
	}

	if dsn := cfg.FormatDSN(); !strings.Contains(dsn, "charset=utf8mb4") {
		t.Errorf("FormatDSN 没带 charset=utf8mb4（dsn=%q）", dsn)
	}
}
