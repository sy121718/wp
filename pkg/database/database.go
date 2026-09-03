// Package database 提供数据库初始化、连接获取与生命周期管理。
package database

import (
	"fmt"
	"strings"
	"sync"
	"time"

	dbdriver "go_wp/pkg/database/driver"
	"go_wp/pkg/logger"

	"github.com/spf13/viper"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	defaultDatabaseDriver       = "postgres"
	defaultDatabaseHost         = "127.0.0.1"
	defaultDatabasePort         = 5432
	defaultDatabaseUser         = "root"
	defaultDatabasePassword     = ""
	defaultDatabaseName         = ""
	defaultDatabaseMaxIdleConns = 10
	defaultDatabaseMaxOpenConns = 100
)

// Config 数据库配置。
type Config struct {
	Driver                 string         `mapstructure:"driver"`
	Host                   string         `mapstructure:"host"`
	Port                   int            `mapstructure:"port"`
	User                   string         `mapstructure:"user"`
	Password               string         `mapstructure:"password"`
	DBName                 string         `mapstructure:"dbname"`
	MaxIdleConns           int            `mapstructure:"max_idle_conns"`
	MaxOpenConns           int            `mapstructure:"max_open_conns"`
	LogLevel               string         `mapstructure:"log_level"`
	PrepareStmt            bool           `mapstructure:"prepare_stmt"`
	SkipDefaultTransaction bool           `mapstructure:"skip_default_transaction"`
	SlowThreshold          string         `mapstructure:"slow_threshold"`
	Resolver               ResolverConfig `mapstructure:"resolver"`
}

// ResolverConfig 预留数据库读写分离配置结构。
//
// 当前阶段：
// - 仅保留配置结构，不启用真正的读写分离逻辑
// -TODO 目的是先把配置边界稳定下来，避免未来再改配置结构
type ResolverConfig struct {
	Enabled  bool     `mapstructure:"enabled"`
	Policy   string   `mapstructure:"policy"`
	Sources  []string `mapstructure:"sources"`
	Replicas []string `mapstructure:"replicas"`
}

type runtimeOptions struct {
	prepareStmt            bool
	skipDefaultTransaction bool
	slowThreshold          time.Duration
}

var (
	db     *gorm.DB
	mu     sync.RWMutex
	inited bool
)

// GetDB 获取数据库实例；未初始化时返回错误。
func GetDB() (*gorm.DB, error) {
	mu.RLock()
	defer mu.RUnlock()

	if db == nil {
		return nil, fmt.Errorf("数据库未初始化，请先调用 database.Init()")
	}
	return db, nil
}

// Init 初始化数据库。
func Init(v *viper.Viper) error {
	mu.Lock()
	defer mu.Unlock()

	if inited {
		return nil
	}
	strict := v != nil && strings.EqualFold(strings.TrimSpace(v.GetString("server.mode")), "release")
	if err := ValidateConfig(v, strict); err != nil {
		return err
	}

	instance, err := initDB(v)
	if err != nil {
		return fmt.Errorf("数据库初始化失败: %w", err)
	}

	db = instance
	inited = true
	return nil
}

// IsInited 检查是否已初始化。
func IsInited() bool {
	mu.RLock()
	defer mu.RUnlock()
	return inited && db != nil
}

// Ready 检查数据库组件是否处于可用状态。
func Ready() error {
	instance, err := GetDB()
	if err != nil {
		return err
	}

	sqlDB, err := instance.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("数据库连接不可用: %w", err)
	}
	return nil
}

func getDefaultConfig() Config {
	return Config{
		Driver:                 defaultDatabaseDriver,
		Host:                   defaultDatabaseHost,
		Port:                   defaultDatabasePort,
		User:                   defaultDatabaseUser,
		Password:               defaultDatabasePassword,
		DBName:                 defaultDatabaseName,
		MaxIdleConns:           defaultDatabaseMaxIdleConns,
		MaxOpenConns:           defaultDatabaseMaxOpenConns,
		LogLevel:               "",
		PrepareStmt:            false,
		SkipDefaultTransaction: false,
		SlowThreshold:          "",
		Resolver: ResolverConfig{
			Enabled:  false,
			Policy:   "",
			Sources:  []string{},
			Replicas: []string{},
		},
	}
}

func resolveLogLevel(serverMode string, dbLogLevel string) gormlogger.LogLevel {
	switch strings.ToLower(dbLogLevel) {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "warn", "warning":
		return gormlogger.Warn
	case "info":
		return gormlogger.Info
	}

	switch strings.ToLower(serverMode) {
	case "release":
		return gormlogger.Warn
	case "test":
		return gormlogger.Error
	default:
		return gormlogger.Info
	}
}

func toDriverConfig(cfg Config) dbdriver.Config {
	return dbdriver.Config{
		Driver:       cfg.Driver,
		Host:         cfg.Host,
		Port:         cfg.Port,
		User:         cfg.User,
		Password:     cfg.Password,
		DBName:       cfg.DBName,
		MaxIdleConns: cfg.MaxIdleConns,
		MaxOpenConns: cfg.MaxOpenConns,
	}
}

func buildDialector(cfg Config) (gorm.Dialector, error) {
	driverCfg := dbdriver.NormalizeConfig(toDriverConfig(cfg))
	switch driverCfg.Driver {
	case "mysql":
		return dbdriver.OpenMySQL(driverCfg), nil
	case "postgres", "postgresql":
		return dbdriver.OpenPostgres(driverCfg), nil
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s", driverCfg.Driver)
	}
}

func initDB(v *viper.Viper) (*gorm.DB, error) {
	cfg := getDefaultConfig()
	if v != nil {
		if err := v.UnmarshalKey("database", &cfg); err != nil {
			logger.Scene("init").With("err", err).Warn("解析数据库配置失败，使用默认配置")
			cfg = getDefaultConfig()
		}
	}

	driverCfg := dbdriver.NormalizeConfig(toDriverConfig(cfg))
	cfg.Driver = driverCfg.Driver
	cfg.Host = driverCfg.Host
	cfg.Port = driverCfg.Port
	cfg.User = driverCfg.User
	cfg.Password = driverCfg.Password
	cfg.DBName = driverCfg.DBName
	cfg.MaxIdleConns = driverCfg.MaxIdleConns
	cfg.MaxOpenConns = driverCfg.MaxOpenConns

	dialector, err := buildDialector(cfg)
	if err != nil {
		return nil, err
	}

	runtimeCfg, err := parseRuntimeOptions(v)
	if err != nil {
		return nil, err
	}

	sqlScene := ""
	serverMode := ""
	if v != nil {
		if v.GetBool("log.capture.sql") {
			sqlScene = "sql"
		}
		serverMode = v.GetString("server.mode")
	}

	gormBaseLogger := gormlogger.Default.LogMode(resolveLogLevel(serverMode, cfg.LogLevel))
	gormDB, err := gorm.Open(dialector, &gorm.Config{
		Logger: newSceneGormLogger(gormBaseLogger, sqlScene, runtimeCfg.slowThreshold),
		// 启用方言错误翻译，便于通过 gorm.ErrDuplicatedKey / gorm.ErrForeignKeyViolated 统一判断。
		TranslateError:         true,
		PrepareStmt:            runtimeCfg.prepareStmt,
		SkipDefaultTransaction: runtimeCfg.skipDefaultTransaction,
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	logger.Scene("init").With("driver", strings.ToLower(cfg.Driver)).Info("数据库初始化成功")
	return gormDB, nil
}

func parseRuntimeOptions(v *viper.Viper) (runtimeOptions, error) {
	options := runtimeOptions{
		prepareStmt:            false,
		skipDefaultTransaction: false,
		slowThreshold:          defaultSQLSlowThreshold,
	}
	if v == nil {
		return options, nil
	}

	options.prepareStmt = v.GetBool("database.prepare_stmt")
	options.skipDefaultTransaction = v.GetBool("database.skip_default_transaction")
	rawThreshold := strings.TrimSpace(v.GetString("database.slow_threshold"))
	if rawThreshold != "" {
		duration, err := time.ParseDuration(rawThreshold)
		if err != nil {
			return runtimeOptions{}, fmt.Errorf("解析 database.slow_threshold 失败: %w", err)
		}
		options.slowThreshold = duration
	}
	return options, nil
}

// ValidateConfig 校验数据库配置。
//
// strict=true 时，会拒绝危险默认值和空关键配置。
func ValidateConfig(v *viper.Viper, strict bool) error {
	cfg := getDefaultConfig()
	if v != nil {
		if err := v.UnmarshalKey("database", &cfg); err != nil {
			return fmt.Errorf("解析数据库配置失败: %w", err)
		}
	}

	if !strict {
		return nil
	}

	if cfg.DBName == "" {
		return fmt.Errorf("database.dbname 不能为空")
	}
	if cfg.DBName == defaultDatabaseName {
		return fmt.Errorf("database.dbname 不能使用默认值")
	}
	if cfg.Password == "" {
		return fmt.Errorf("database.password 不能为空")
	}
	return nil
}

// Close 关闭数据库连接。
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if db == nil {
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return err
	}

	db = nil
	inited = false
	return nil
}
