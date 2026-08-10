package dbdriver

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// OpenPostgres 创建 PostgreSQL Dialector
func OpenPostgres(cfg Config) gorm.Dialector {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=Asia/Shanghai",
		cfg.Host,
		cfg.User,
		cfg.Password,
		cfg.DBName,
		cfg.Port,
	)
	return postgres.Open(dsn)
}
