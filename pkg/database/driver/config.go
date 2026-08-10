package dbdriver

import "strings"

// Config 数据库驱动配置
type Config struct {
	Driver       string
	Host         string
	Port         int
	User         string
	Password     string
	DBName       string
	MaxIdleConns int
	MaxOpenConns int
}

// NormalizeConfig 归一化数据库配置
func NormalizeConfig(cfg Config) Config {
	defaultCfg := Config{
		Driver:       "mysql",
		Host:         "127.0.0.1",
		Port:         3306,
		User:         "root",
		Password:     "",
		DBName:       "test",
		MaxIdleConns: 10,
		MaxOpenConns: 100,
	}

	if cfg.Driver == "" {
		cfg.Driver = defaultCfg.Driver
	}
	if cfg.Host == "" {
		cfg.Host = defaultCfg.Host
	}
	if cfg.Port == 0 {
		cfg.Port = defaultCfg.Port
	}
	if cfg.User == "" {
		cfg.User = defaultCfg.User
	}
	if cfg.DBName == "" {
		cfg.DBName = defaultCfg.DBName
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = defaultCfg.MaxIdleConns
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = defaultCfg.MaxOpenConns
	}
	cfg.Driver = strings.ToLower(cfg.Driver)
	return cfg
}
