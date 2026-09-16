package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/sitehttps"

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
	// DebugAllowPublic 是否允许 debug/test 模式绑定非环回地址（server.debug_allow_public）。
	//
	// 默认 false：debug/test 只监听 127.0.0.1。理由见 cmd/main.go 的 listenAddr ——
	// debug 模式的放宽项（一键登录、CORS 反射任意 Origin、信任所有代理因而采信
	// X-Forwarded-For）任意一条落在公网可达的地址上都是完整的入侵路径。
	// 要用手机 / 局域网设备访问本地开发环境时才显式打开。
	DebugAllowPublic bool
}

// envBindableKeys 允许用环境变量覆盖的配置键。
//
// 环境变量名由 SetEnvPrefix("GOWP") + SetEnvKeyReplacer(".", "_") 从键名推导：
// database.password → GOWP_DATABASE_PASSWORD。
//
// 只列这些键，是因为它们都有「换个环境就得换值」或「不该落盘明文」的性质 ——
// 部署时注入密码与密钥、CI 里把连接指向服务容器，都靠它们。其余配置是部署资产，
// 留在配置文件里更清楚，也更不容易被一次 export 意外改掉。
var envBindableKeys = []string{
	// 数据库连接
	"database.host",
	"database.port",
	"database.user",
	"database.password",
	"database.dbname",
	// 会话存储
	"redis.host",
	"redis.port",
	"redis.password",
	"redis.db",
	// 密钥（配置文件里留空、由部署注入是推荐做法）
	"auth.session_secret",
	"app.secret",
	"analytics.pepper",
}

// applyEnvOverrides 把已设置的环境变量并入所属顶层段，再整段写回 Viper。
//
// 为什么不能只靠 AutomaticEnv（哪怕配合 BindEnv）：AutomaticEnv 作用于 Get* 系列，
// 而本项目的配置读取走 UnmarshalKey，它内部是 decode(Get(段名)) —— 返回的是配置文件
// 里的那棵子树，逐个子键绑定不会出现在子树中。结果是环境变量被静默忽略：没有报错、
// 退出码 0，只是配置没换。实测反例：GOWP_DATABASE_DBNAME 指向一个不存在的库，
// -migrate-only 照旧迁移 config.yaml 里写的那个库并成功返回。
//
// 合并成段级 map 再 Set 回去，Get("database") 命中 override 里的整段，
// UnmarshalKey 才读得到。
func applyEnvOverrides(cfg *viper.Viper) error {
	merged := make(map[string]map[string]any)

	for _, key := range envBindableKeys {
		envName := "GOWP_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))

		raw, ok := os.LookupEnv(envName)
		// 空值按未设置处理：VAR= 在 shell 与 CI 里太容易意外出现，
		// 真要用空值覆盖的场景（清空 redis 密码之类）由配置文件表达更明确。
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}

		section, field, found := strings.Cut(key, ".")
		if !found {
			return fmt.Errorf("环境变量覆盖键 %s 不是 段.字段 形式", key)
		}

		if merged[section] == nil {
			// 必须复制：GetStringMap 对 map[string]any 直接返回内部引用，
			// 就地修改会污染 Viper 里的配置树。
			src := cfg.GetStringMap(section)
			dst := make(map[string]any, len(src)+1)
			for k, val := range src {
				dst[k] = val
			}
			merged[section] = dst
		}
		merged[section][field] = raw
	}

	for section, values := range merged {
		cfg.Set(section, values)
	}
	return nil
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

	// OSS-014：环境变量覆盖 YAML（GOWP_DATABASE_PASSWORD → database.password）。
	cfg.SetEnvPrefix("GOWP")
	cfg.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	cfg.AutomaticEnv()

	// 只开 AutomaticEnv 是不够的：它只作用于 Get* 系列，而配置读取走 UnmarshalKey
	// （server / database / auth / app …），Unmarshal 遍历的是 AllKeys ——
	// 配置文件键 + 默认值 + **显式绑定**，不含纯环境变量键。
	// 实测过这个缺口：GOWP_DATABASE_DBNAME 指向不存在的库，迁移照旧落在 config.yaml
	// 写的那个库上，退出码 0 —— 环境变量被静默忽略，看上去一切正常。
	// 显式 BindEnv 后这些键进入 AllKeys，Unmarshal 才读得到（env 优先级高于配置文件，
	// 语义正是「覆盖」）；键在配置文件里不存在也无妨，绑定本身就是让它存在。
	for _, key := range envBindableKeys {
		if err := cfg.BindEnv(key); err != nil {
			return fmt.Errorf("绑定环境变量覆盖键 %s 失败: %w", key, err)
		}
	}
	if err := applyEnvOverrides(cfg); err != nil {
		return err
	}

	v = cfg
	// 站点协议判定（cookie 的 Secure 属性）由 pkg/sitehttps 承担：它不反向 import
	// 本包（config → pkg/auth → 这里会成环），所以在这里单向注入配置实例。
	sitehttps.Init(cfg)
	logger.Scene("init").With("path", configPath).Info("配置加载成功")
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
		DebugAllowPublic  bool   `mapstructure:"debug_allow_public"`
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
		DebugAllowPublic:  raw.DebugAllowPublic,
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
	sitehttps.ResetForTest()
}
