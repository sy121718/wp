package config

// migrations_switch.go — 「启动时是否执行结构迁移与业务 seed」的显式开关（DB-009 切角色前置）。
//
// 背景：migrations 组件是 Critical 组件，启动链上无条件执行 DDL 与 seed。业务连接换成
// 非超级角色（go_wp_app，NOSUPERUSER / 不是任何表的属主、public schema 上无 CREATE）
// 之后，连 CREATE TABLE IF NOT EXISTS 都会被拒（permission denied for schema public），
// 应用会在启动阶段直接挂掉。而「迁移走管理连接」本来就有既有入口：cmd/main.go 的
// -migrate-only。缺的只是「启动时别再迁一次」这个开关。
//
// 语义边界：
//   - database.run_migrations 缺省 true，即**保持既有行为** —— 旧配置文件里没有这个键，
//     不写就等于没改；
//   - 置 false 只跳过 migrations 组件与两处 migrations.RunSeeds（cmd/main.go 的
//     -migrate-only、internal/routers/assembly.go 的启动 seed）。
//     permission.SyncToDB 的权限点 upsert **不属于 seed**，每次启动照跑（见 register.go
//     末尾与 assembly.go 装配段）；
//   - -migrate-only 是**显式的迁移命令**，它不受这个开关约束（否则「用管理连接跑迁移」
//     这条推荐流程会被自己的配置关掉，变成静默什么都没做）。命令行为见 cmd/main.go：
//     解析到该 flag 后调用 ForceMigrations()。

import (
	"sync"
	"sync/atomic"

	"github.com/spf13/viper"

	"go_wp/pkg/logger"
)

// keyRunMigrations 配置键（database 段）。
const keyRunMigrations = "database.run_migrations"

// forceMigrations 由 -migrate-only 置位：显式迁移命令忽略 database.run_migrations。
var forceMigrations atomic.Bool

// ForceMigrations 让本进程忽略 database.run_migrations，强制执行结构迁移与 seed。
//
// 只应由 -migrate-only 调用：那是一次显式的迁移命令，不该被「服务启动时是否迁移」的
// 开关关掉。必须在 config.InitComponents() 之前调用。
func ForceMigrations() { forceMigrations.Store(true) }

// forceMigrationsSet 供测试复位（生产路径只置位、不复位）。
func forceMigrationsSet(v bool) { forceMigrations.Store(v) }

var migrationsSkipLogged sync.Once

// logMigrationsSkipped 跳过时打一条 INFO，且只打一次。
//
// 为什么必须留痕：跳过迁移的代价不在启动日志里 —— 少建一张表只会在运行期以
// 「relation does not exist」或「查不到数据」的形式出现，没人会把两件事联起来。
func logMigrationsSkipped() {
	migrationsSkipLogged.Do(func() {
		logger.Scene("init").Info("按 database.run_migrations=false 跳过结构与 seed 迁移，请确认已用管理连接执行过 -migrate-only")
	})
}

// runMigrationsEnabled 是开关的唯一判定处（cfg 版本，供组件 Enabled 谓词使用）。
func runMigrationsEnabled(cfg *viper.Viper) bool {
	if forceMigrations.Load() {
		return true
	}
	// 显式判断「键是否存在」而不是直接 GetBool：GetBool 对缺失键返回 false，
	// 那会把「没写这个键」误读成「关掉迁移」，与默认 true 的约定正好相反。
	if cfg == nil || !cfg.IsSet(keyRunMigrations) {
		return true
	}
	return cfg.GetBool(keyRunMigrations)
}

// RunMigrationsEnabled 启动链（cmd/main.go、internal/routers）判断是否执行迁移与 seed。
//
// 取不到 Viper（配置未初始化）时返回 true：那属于装配缺陷，不该顺带把迁移也关掉。
func RunMigrationsEnabled() bool {
	cfg, err := GetViper()
	if err != nil {
		return true
	}
	if runMigrationsEnabled(cfg) {
		return true
	}
	logMigrationsSkipped()
	return false
}
