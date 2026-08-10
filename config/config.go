package config

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

var (
	v  *viper.Viper
	mu sync.Mutex
)

type ServerConfig struct {
	Port              int
	Mode              string
	AppName           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	RequestBodyLimit  int64
	UploadBodyLimit   int64
	RateLimitEnabled  bool
	RateLimitLimit    int
	RateLimitWindow   time.Duration
	PortStrategy      string
}

func Init(configPath string) error {
	mu.Lock()
	defer mu.Unlock()

	if v != nil {
		return nil
	}

	cfg := viper.New()
	cfg.SetConfigFile(configPath)

	if err := cfg.ReadInConfig(); err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	v = cfg
	log.Printf("配置加载成功: %s", configPath)
	return nil
}

func GetViper() (*viper.Viper, error) {
	if v == nil {
		return nil, fmt.Errorf("配置未初始化，请先调用 config.Init()")
	}
	return v, nil
}

func GetServer() (ServerConfig, error) {
	type serverConfigRaw struct {
		Port              int    `mapstructure:"port"`
		Mode              string `mapstructure:"mode"`
		AppName           string `mapstructure:"app_name"`
		ReadHeaderTimeout string `mapstructure:"read_header_timeout"`
		ReadTimeout       string `mapstructure:"read_timeout"`
		WriteTimeout      string `mapstructure:"write_timeout"`
		IdleTimeout       string `mapstructure:"idle_timeout"`
		RequestBodyLimit  string `mapstructure:"request_body_limit"`
		UploadBodyLimit   string `mapstructure:"upload_body_limit"`
		RateLimitEnabled  bool   `mapstructure:"rate_limit_enabled"`
		RateLimitLimit    int    `mapstructure:"rate_limit_limit"`
		RateLimitWindow   string `mapstructure:"rate_limit_window"`
		PortStrategy      string `mapstructure:"port_strategy"`
	}

	var raw serverConfigRaw
	cfg, err := GetViper()
	if err != nil {
		return ServerConfig{}, err
	}
	if err := cfg.UnmarshalKey("server", &raw); err != nil {
		return ServerConfig{}, fmt.Errorf("解析 Server 配置失败: %w", err)
	}

	readHeaderTimeout, err := parseServerDuration("read_header_timeout", raw.ReadHeaderTimeout)
	if err != nil {
		return ServerConfig{}, err
	}
	readTimeout, err := parseServerDuration("read_timeout", raw.ReadTimeout)
	if err != nil {
		return ServerConfig{}, err
	}
	writeTimeout, err := parseServerDuration("write_timeout", raw.WriteTimeout)
	if err != nil {
		return ServerConfig{}, err
	}
	idleTimeout, err := parseServerDuration("idle_timeout", raw.IdleTimeout)
	if err != nil {
		return ServerConfig{}, err
	}
	requestBodyLimit, err := parseByteSize("request_body_limit", raw.RequestBodyLimit)
	if err != nil {
		return ServerConfig{}, err
	}
	uploadBodyLimit, err := parseByteSize("upload_body_limit", raw.UploadBodyLimit)
	if err != nil {
		return ServerConfig{}, err
	}
	rateLimitWindow, err := parseServerDuration("rate_limit_window", raw.RateLimitWindow)
	if err != nil {
		return ServerConfig{}, err
	}
	if raw.RateLimitLimit <= 0 {
		return ServerConfig{}, fmt.Errorf("解析 server.rate_limit_limit 失败: 值必须大于 0")
	}

	return ServerConfig{
		Port:              raw.Port,
		Mode:              raw.Mode,
		AppName:           raw.AppName,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		RequestBodyLimit:  requestBodyLimit,
		UploadBodyLimit:   uploadBodyLimit,
		RateLimitEnabled:  raw.RateLimitEnabled,
		RateLimitLimit:    raw.RateLimitLimit,
		RateLimitWindow:   rateLimitWindow,
		PortStrategy:      raw.PortStrategy,
	}, nil
}

func parseServerDuration(field string, raw string) (time.Duration, error) {
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("解析 server.%s 失败: %w", field, err)
	}
	return duration, nil
}

func parseByteSize(field string, raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("解析 server.%s 失败: 值不能为空", field)
	}

	normalized := strings.ToUpper(strings.TrimSpace(raw))
	units := []struct {
		Suffix string
		Scale  int64
	}{
		{Suffix: "KB", Scale: 1024},
		{Suffix: "MB", Scale: 1024 * 1024},
		{Suffix: "GB", Scale: 1024 * 1024 * 1024},
		{Suffix: "B", Scale: 1},
	}

	for _, unit := range units {
		if strings.HasSuffix(normalized, unit.Suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(normalized, unit.Suffix))
			value, err := strconv.ParseInt(number, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("解析 server.%s 失败: %w", field, err)
			}
			if value <= 0 {
				return 0, fmt.Errorf("解析 server.%s 失败: 值必须大于 0", field)
			}
			return value * unit.Scale, nil
		}
	}

	value, err := strconv.ParseInt(normalized, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析 server.%s 失败: %w", field, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("解析 server.%s 失败: 值必须大于 0", field)
	}
	return value, nil
}

func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	v = nil
}
