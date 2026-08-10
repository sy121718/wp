package dbdriver

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenMySQL 创建 MySQL Dialector
func OpenMySQL(cfg Config) gorm.Dialector {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
	)
	return mysql.Open(dsn)
}
