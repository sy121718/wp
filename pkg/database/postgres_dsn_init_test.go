package database

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/viper"
)

func skipOrFailPG(t *testing.T, err error) {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("GOWP_REQUIRE_PG")); v == "1" || strings.EqualFold(v, "true") {
		t.Fatalf("PostgreSQL 不可用，而 GOWP_REQUIRE_PG=1 要求本次测试必须真跑：%v", err)
	}
	t.Skipf("本机没有可用的 PG（跳过）：%v", err)
}

func pgTestParams() (host, port, user, password, dbname string) {
	envOr := func(key, fallback string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	return envOr("PGHOST", "127.0.0.1"),
		envOr("PGPORT", "5432"),
		envOr("PGUSER", "root"),
		envOr("PGPASSWORD", "root"),
		envOr("PGDATABASE", "wp")
}

func testPGConfig() *viper.Viper {
	host, port, user, password, dbname := pgTestParams()
	v := viper.New()
	v.Set("database.driver", "postgres")
	v.Set("database.host", host)
	v.Set("database.port", port)
	v.Set("database.user", user)
	v.Set("database.password", password)
	v.Set("database.dbname", dbname)
	return v
}

func TestInitSucceedsWithEmptyPasswordAndKeepsTimeZone(t *testing.T) {
	cfg := testPGConfig()
	wantDB := cfg.GetString("database.dbname")
	if err := Init(cfg); err != nil {

		if probeErr := probePGReachable(cfg); probeErr != nil {
			skipOrFailPG(t, probeErr)
		}
		t.Fatalf("库连得上，但 Init 失败 —— 这是 DSN 组装缺陷，不是环境问题：%v", err)
	}
	db, err := GetDB()
	if err != nil {
		t.Fatalf("Init 成功但 GetDB 失败: %v", err)
	}

	var timeZone, dbName string
	if err = db.Raw("SHOW TimeZone").Scan(&timeZone).Error; err != nil {
		t.Fatalf("SHOW TimeZone 失败: %v", err)
	}
	if timeZone != "Asia/Shanghai" {
		t.Errorf("会话时区是 %q，应为 Asia/Shanghai —— TimeZone 没生效（被 URL 编码后 gorm 的 LoadLocation 会直接失败）", timeZone)
	}

	if err = db.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil {
		t.Fatalf("查询当前库名失败: %v", err)
	}
	if dbName != wantDB {
		t.Errorf("连到了库 %q，应为 %q —— 空密码吃掉了 dbname", dbName, wantDB)
	}
}

func probePGReachable(cfg *viper.Viper) error {
	host := cfg.GetString("database.host")
	port := cfg.GetInt("database.port")
	user := cfg.GetString("database.user")
	password := cfg.GetString("database.password")
	dbname := cfg.GetString("database.dbname")

	conn, err := pgx.Connect(context.Background(), probePGDSN(host, port, user, password, dbname))
	if err != nil {
		return err
	}
	return conn.Close(context.Background())
}

func probePGDSN(host string, port int, user, password, dbname string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + dbname,
	}
	query := u.Query()
	query.Set("sslmode", "disable")
	u.RawQuery = query.Encode()
	return u.String()
}

func TestProbePGDSNKeepsIdentity(t *testing.T) {
	const password = "p@ss:w/rd#x"
	parsed, err := pgx.ParseConfig(probePGDSN("127.0.0.1", 5432, "probe", password, "probe"))
	if err != nil {
		t.Fatalf("探针串无法解析（口令里的特殊字符把 URL 切坏了）: %v", err)
	}
	if parsed.Host != "127.0.0.1" || parsed.Port != 5432 {
		t.Errorf("地址解析成 %s:%d，应为 127.0.0.1:5432", parsed.Host, parsed.Port)
	}
	if parsed.User != "probe" {
		t.Errorf("用户解析成 %q", parsed.User)
	}
	if parsed.Password != password {
		t.Errorf("口令解析成 %q，应为 %q —— 探针丢了口令，「库在、口令错」会被当成「库不在」而 Skip", parsed.Password, password)
	}
	if parsed.Database != "probe" {
		t.Errorf("库名解析成 %q", parsed.Database)
	}
}
