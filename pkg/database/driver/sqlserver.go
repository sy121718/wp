package dbdriver

import (
	"fmt"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// OpenSQLServer 创建 SQL Server Dialector
func OpenSQLServer(cfg Config) gorm.Dialector {
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
	)
	return sqlserver.Open(dsn)
}
