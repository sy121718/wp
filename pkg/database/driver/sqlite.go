package dbdriver

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// OpenSQLite 创建 SQLite Dialector
func OpenSQLite(cfg Config) gorm.Dialector {
	return sqlite.Open(cfg.DBName)
}
