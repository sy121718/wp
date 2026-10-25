package dbdriver

import (
	"strconv"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenMySQL 创建 MySQL Dialector。
func OpenMySQL(cfg Config) gorm.Dialector {
	return mysql.New(mysql.Config{DSNConfig: MySQLDSNConfig(cfg)})
}

// MySQLDSNConfig 把驱动配置转成 go-sql-driver 的 Config。
//
// 为什么不像旧实现那样 `fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?...")` 拼字符串：
// 实测确认出事的是**库名与 query 段**，不是密码：
//
//	库名 db/x            => ParseDSN 直接失败（invalid DSN: did you forget to escape a param value?）
//	库名 my?db           => 静默截断成 my，后面的串被当成 DSN 参数
//	库名 db?charset=latin1 => 截断 + 改写查询参数
//
// 交给 go-sql-driver 自己的 Config + FormatDSN 处理：转义规则由解析方负责，两边不可能漂。
//
// 除连接信息外还要两个驱动选项：parseTime（DATETIME 扫成 time.Time）与 loc（时区
// 口径，与 PG 侧的 TimeZone 对齐）；charset 走 mysql.Charset 选项 ——
// 新版驱动把 charset 存在私有字段 charsets 里，塞 Params["charset"] 会在 FormatDSN
// 时被静默丢掉（实测：往返后 charset 变空）。
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
		// Charset 选项在当前实现里恒返回 nil；真出错时宁可没 charset 也不 panic
		// （连接仍可用，只是服务端默认字符集）。
		return c
	}
	return c
}

// timeLocal 与旧 DSN 里 `loc=Local` 同义（服务端与进程同处一机时的本地时区）。
//
// 用 LoadLocation("Local") 而不是 time.Local 字面量：Config.FormatDSN 要把
// Location 转成字符串（`loc=`），time.Local 的 String() 在不同平台是 "Local" /
// 本地时区名，二者不总是协议认可的写法。
var timeLocal = func() *time.Location {
	loc, err := time.LoadLocation("Local")
	if err != nil {
		return time.UTC
	}
	return loc
}()
