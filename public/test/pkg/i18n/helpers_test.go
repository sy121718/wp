package i18n_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/spf13/viper"

	"go_wp/public/test/support"
)

func i18nTestDSN(dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		support.DefaultPGHost, support.DefaultPGUser, support.DefaultPGPassword, dbname, support.DefaultPGPort)
}

func newI18nTestConfig(dbname string) *viper.Viper {
	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", dbname)
	cfg.Set("database.host", support.DefaultPGHost)
	cfg.Set("database.port", 5432)
	cfg.Set("database.user", support.DefaultPGUser)
	cfg.Set("database.password", support.DefaultPGPassword)
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 2)
	cfg.Set("i18n.default_lang", "zh-CN")
	cfg.Set("i18n.auto_refresh", false)
	return cfg
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
