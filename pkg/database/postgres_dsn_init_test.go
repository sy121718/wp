package database

// postgres_dsn_init_test.go — 连接串组装在**真实入口**上的回归判据。
//
// 为什么这一层要单独一条：dbdriver 的 DSN 单测验的是字符串与 pgx 解析，
// 而 gorm 驱动在 pgx 之外还做了一件事 —— 对 DSN **原文**跑正则取 TimeZone，
// 再把值交给 time.LoadLocation（不经 URL 解码）。于是「TimeZone 被编码成
// Asia%2FShanghai」这种缺陷能穿过所有字符串级断言，直到这里才炸：所有部署启不动。
//
// 本文件走 database.Init（生产启动路径），是这条缺陷唯一能被人看到的地方。

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/viper"
)

// skipOrFailPG 依赖不可用时的口径：宽松模式 Skip，严格模式（GOWP_REQUIRE_PG=1）必须红。
//
// 为什么 pkg 层也要守这一条：严格模式的意义是「失败不得被伪装成跳过」。只写 t.Skipf 时，
// `GOWP_REQUIRE_PG=1 go test ./...` 会把「库不在」报成 ok，看起来全绿，实际一条真连接都没跑。
func skipOrFailPG(t *testing.T, err error) {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("GOWP_REQUIRE_PG")); v == "1" || strings.EqualFold(v, "true") {
		t.Fatalf("PostgreSQL 不可用，而 GOWP_REQUIRE_PG=1 要求本次测试必须真跑：%v", err)
	}
	t.Skipf("本机没有可用的 PG（跳过）：%v", err)
}

// pgTestParams 真实连接用的参数：默认对齐 config.yaml.example，可用标准 libpq
// 环境变量（PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE）覆盖。
//
// **不读 config.yaml**：仓库只有 config.yaml.example，测试不该依赖开发者本地配置。
//
// 以前这里把库名写死成 `artex` —— 那是另一个项目的库，与本仓无关。写死它有两个后果：
// 换一台机器（含 CI）就没有这个库，测试永久 Skip 或被判失败；而恰好装了它的机器上，
// 「库名没被空密码吃掉」这条断言验的是别人的库。库名必须是本项目的库。
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
		os.Getenv("PGPASSWORD"), // 默认空口令：本地无密码账号很常见，正是本文件要钉的场景
		envOr("PGDATABASE", "wp")
}

// testPGConfig 本机可用的连接参数（默认无密码账号）。
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

// TestInitSucceedsWithEmptyPasswordAndKeepsTimeZone 空密码 + 时区在完整启动链路上都正确。
//
// 两个断言各钉一个缺陷面：
//   - TimeZone 生效：证明 options/TimeZone 的编码方式没有让 gorm 的 LoadLocation 失败；
//   - 库名正确：证明空密码没吃掉 dbname（旧实现的原始缺陷）。
func TestInitSucceedsWithEmptyPasswordAndKeepsTimeZone(t *testing.T) {
	cfg := testPGConfig()
	wantDB := cfg.GetString("database.dbname")
	if err := Init(cfg); err != nil {
		// **Skip 只用于「库不在」**。DSN 组装缺陷也会让 Init 返回 error（典型形态：
		// TimeZone 被编码成 Asia%2FShanghai 后 gorm 的 LoadLocation 失败），
		// 那正是本测试存在的理由 —— 把它当环境问题跳过，等于这条测试从未存在。
		// 用「最朴素的连接能不能建立」区分两者。
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

// probePGReachable 用最朴素的连接串探一次库是否可达（不经任何被检的组装逻辑）。
//
// 只用于把「环境没有库」与「我们组装的串有问题」分开：前者 Skip，后者必须红。
// 口令非空时必须带上 —— 否则「库在、口令错」会被误判成「库不在」而 Skip。
func probePGReachable(cfg *viper.Viper) error {
	host := cfg.GetString("database.host")
	port := cfg.GetInt("database.port")
	user := cfg.GetString("database.user")
	password := cfg.GetString("database.password")
	dbname := cfg.GetString("database.dbname")

	auth := user
	if password != "" {
		auth = user + ":" + password
	}
	probe := fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable", auth, host, port, dbname)
	conn, err := pgx.Connect(context.Background(), probe)
	if err != nil {
		return err
	}
	return conn.Close(context.Background())
}
