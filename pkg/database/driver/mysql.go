package dbdriver

import (
	"strconv"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func OpenMySQL(cfg Config) gorm.Dialector {
	return mysql.New(mysql.Config{DSNConfig: MySQLDSNConfig(cfg)})
}

func MySQLDSNConfig(cfg Config) *mysqldriver.Config {
	c := mysqldriver.NewConfig()
	c.User = cfg.User
	c.Passwd = cfg.Password
	c.Net = "tcp"
	c.Addr = cfg.Host + ":" + strconv.Itoa(cfg.Port)
	c.DBName = cfg.DBName
	c.ParseTime = true
	c.Loc = timeLocal
	if err := mysqldriver.Charset("utf8mb4", "")(c); err != nil {

		return c
	}
	return c
}

var timeLocal = func() *time.Location {
	loc, err := time.LoadLocation("Local")
	if err != nil {
		return time.UTC
	}
	return loc
}()
