package config

import (
	"sync"
	"sync/atomic"

	"github.com/spf13/viper"

	"go_wp/pkg/logger"
)

const keyRunMigrations = "database.run_migrations"

var forceMigrations atomic.Bool

func ForceMigrations() { forceMigrations.Store(true) }

func forceMigrationsSet(v bool) { forceMigrations.Store(v) }

const keyRunWorker = "queue.run_worker"

var suppressWorkers atomic.Bool

func SuppressWorkers() { suppressWorkers.Store(true) }

var migrationsSkipLogged sync.Once

func logMigrationsSkipped() {
	migrationsSkipLogged.Do(func() {
		logger.Scene("init").Info("按 database.run_migrations=false 跳过结构与 seed 迁移，请确认已用管理连接执行过 -migrate-only")
	})
}

func runMigrationsEnabled(cfg *viper.Viper) bool {
	if forceMigrations.Load() {
		return true
	}

	if cfg == nil || !cfg.IsSet(keyRunMigrations) {
		return true
	}
	return cfg.GetBool(keyRunMigrations)
}

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
