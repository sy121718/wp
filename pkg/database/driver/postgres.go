package dbdriver

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func OpenPostgres(cfg Config) gorm.Dialector {
	return postgres.Open(PostgresDSN(cfg))
}

func PostgresDSN(cfg Config) string {
	u := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.DBName,
	}

	if cfg.Password == "" {
		u.User = url.User(cfg.User)
	} else {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}

	q := url.Values{}
	q.Set("sslmode", "disable")
	if cfg.StatementTimeout > 0 {
		q.Set("options", "-c statement_timeout="+strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10))
	}
	u.RawQuery = encodePostgresQuery(q) + "&TimeZone=" + postgresTimeZone
	return u.String()
}

const postgresTimeZone = "Asia/Shanghai"

func encodePostgresQuery(q url.Values) string {
	return strings.ReplaceAll(q.Encode(), "+", "%20")
}
