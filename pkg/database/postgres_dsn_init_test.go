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
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/viper"
)

// testPGConfig 本机可用的连接参数（无密码账号）。
//
// 不读 config.yaml：仓库只有 config.yaml.example，测试不该依赖开发者本地配置。
// 连不上时 t.Skip（本仓口径：PG 不可用则跳过），连上了就必须全部断言通过。
func testPGConfig() *viper.Viper {
	v := viper.New()
	v.Set("database.driver", "postgres")
	v.Set("database.host", "127.0.0.1")
	v.Set("database.port", 5432)
	v.Set("database.user", "sky")
	v.Set("database.password", "")
	v.Set("database.dbname", "artex")
	return v
}

// TestInitSucceedsWithEmptyPasswordAndKeepsTimeZone 空密码 + 时区在完整启动链路上都正确。
//
// 两个断言各钉一个缺陷面：
//   - TimeZone 生效：证明 options/TimeZone 的编码方式没有让 gorm 的 LoadLocation 失败；
//   - 库名正确：证明空密码没吃掉 dbname（旧实现的原始缺陷）。
func TestInitSucceedsWithEmptyPasswordAndKeepsTimeZone(t *testing.T) {
	if err := Init(testPGConfig()); err != nil {
		// **Skip 只用于「库不在」**。DSN 组装缺陷也会让 Init 返回 error（典型形态：
		// TimeZone 被编码成 Asia%2FShanghai 后 gorm 的 LoadLocation 失败），
		// 那正是本测试存在的理由 —— 把它当环境问题跳过，等于这条测试从未存在。
		// 用「最朴素的连接能不能建立」区分两者。
		if probeErr := probePGReachable(); probeErr != nil {
			t.Skipf("本机没有可用的 PG（跳过）：%v", probeErr)
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
	if dbName != "artex" {
		t.Errorf("连到了库 %q，应为 artex —— 空密码吃掉了 dbname", dbName)
	}
}

// probePGReachable 用最朴素的连接串探一次库是否可达（不经任何被检的组装逻辑）。
//
// 只用于把「环境没有库」与「我们组装的串有问题」分开：前者 Skip，后者必须红。
func probePGReachable() error {
	conn, err := pgx.Connect(context.Background(),
		"postgres://sky@127.0.0.1:5432/artex?sslmode=disable")
	if err != nil {
		return err
	}
	return conn.Close(context.Background())
}
