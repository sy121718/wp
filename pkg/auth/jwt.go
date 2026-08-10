package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

// Config JWT 配置。
type Config struct {
	Secret     string `mapstructure:"secret"`
	ExpireTime int    `mapstructure:"expire_time"`
	Issuer     string `mapstructure:"issuer"`
}

var (
	jwtSecret []byte
	jwtConfig Config
	inited    bool
	jwtMu     sync.RWMutex
)

const (
	defaultJWTSecret     = "default-secret-key-please-change-in-production"
	defaultJWTExpireTime = 24
	defaultJWTIssuer     = "go_wp"
)

// Claims 自定义 claims。
type Claims struct {
	UserID     int64  `json:"user_id"`
	Username   string `json:"username"`
	RememberMe bool   `json:"remember_me"`
	SessionID  string `json:"session_id"`
	jwt.RegisteredClaims
}

func getDefaultConfig() Config {
	return Config{
		Secret:     defaultJWTSecret,
		ExpireTime: defaultJWTExpireTime,
		Issuer:     defaultJWTIssuer,
	}
}

// ValidateConfig 校验 JWT 配置。
//
// 说明：
// - 用于框架启动前的 fail-fast 校验
// - strict=true 时，会拒绝默认 secret 和空 secret
func ValidateConfig(v *viper.Viper, strict bool) error {
	cfg := getDefaultConfig()
	if v != nil {
		if err := v.UnmarshalKey("jwt", &cfg); err != nil {
			return fmt.Errorf("解析 JWT 配置失败: %w", err)
		}
	}

	if !strict {
		return nil
	}

	if cfg.Secret == "" {
		return fmt.Errorf("jwt.secret 不能为空")
	}
	if cfg.Secret == getDefaultConfig().Secret {
		return fmt.Errorf("jwt.secret 不能使用默认值")
	}
	return nil
}

// Init 初始化 JWT 组件。
func Init(v *viper.Viper) error {
	jwtMu.Lock()
	defer jwtMu.Unlock()

	if inited {
		return nil
	}

	cfg := getDefaultConfig()
	if v != nil {
		if err := v.UnmarshalKey("jwt", &cfg); err != nil {
			return fmt.Errorf("解析 JWT 配置失败: %w", err)
		}
	}
	strict := v != nil && strings.EqualFold(strings.TrimSpace(v.GetString("server.mode")), "release")
	if err := ValidateConfig(v, strict); err != nil {
		return err
	}

	defaultCfg := getDefaultConfig()
	if cfg.Secret == "" {
		cfg.Secret = defaultCfg.Secret
		log.Println("警告: JWT secret 未配置，使用默认值（生产环境请修改）")
	}
	if cfg.ExpireTime <= 0 {
		cfg.ExpireTime = defaultCfg.ExpireTime
	}
	if cfg.Issuer == "" {
		cfg.Issuer = defaultCfg.Issuer
	}

	jwtConfig = cfg
	jwtSecret = []byte(cfg.Secret)
	inited = true
	log.Println("JWT 初始化成功")
	return nil
}

// Close 关闭 JWT 组件并清空运行时状态。
func Close() error {
	jwtMu.Lock()
	defer jwtMu.Unlock()

	jwtSecret = nil
	jwtConfig = Config{}
	inited = false
	return nil
}

// GenerateTokenPair 生成 token 对：accessToken（短期）+ refreshToken（长期）。
//
// accessToken  使用配置的默认过期时间（24 小时）
// refreshToken 勾选记住我时为 7 天（168h），否则与 accessToken 相同
// 返回 accessToken、refreshToken、accessToken 的过期时间字符串。
func GenerateTokenPair(userID int64, username string, rememberMe bool) (accessToken, refreshToken string, expires string, err error) {
	secret, cfg, err := snapshotState()
	if err != nil {
		return "", "", "", err
	}

	now := time.Now()
	accessHours := cfg.ExpireTime
	if accessHours <= 0 {
		accessHours = defaultJWTExpireTime
	}

	refreshHours := accessHours
	if rememberMe {
		refreshHours = 168 // 7 天
	}

	// accessToken
	accessClaims := Claims{
		UserID:     userID,
		Username:   username,
		RememberMe: rememberMe,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(accessHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    cfg.Issuer,
		},
	}
	accessToken, err = jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(secret)
	if err != nil {
		return "", "", "", err
	}

	// refreshToken（用不同过期时间）
	refreshClaims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(refreshHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    cfg.Issuer,
		},
	}
	refreshToken, err = jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(secret)
	if err != nil {
		return "", "", "", err
	}

	expires = now.Add(time.Duration(accessHours) * time.Hour).Format("2006/01/02 15:04:05")
	return accessToken, refreshToken, expires, nil
}

// GenerateToken 生成使用新会话 ID 的 access token。
func GenerateToken(userID int64, username string, rememberMe bool) (string, error) {
	token, _, err := GenerateSessionToken(userID, username, rememberMe, "")
	return token, err
}

// GenerateSessionToken 生成与 Redis 会话绑定的 access token。
// sessionID 为空时创建新会话；自动续期时传入原会话 ID。
func GenerateSessionToken(userID int64, username string, rememberMe bool, sessionID string) (token string, resolvedSessionID string, err error) {
	secret, cfg, err := snapshotState()
	if err != nil {
		return "", "", err
	}
	if sessionID == "" {
		sessionID, err = newSessionID()
		if err != nil {
			return "", "", err
		}
	}

	now := time.Now()
	accessHours := cfg.ExpireTime
	if accessHours <= 0 {
		accessHours = defaultJWTExpireTime
	}

	claims := Claims{
		UserID:     userID,
		Username:   username,
		RememberMe: rememberMe,
		SessionID:  sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(accessHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    cfg.Issuer,
		},
	}

	token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		return "", "", err
	}
	return token, sessionID, nil
}

func newSessionID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// ParseToken 解析 Token。
func ParseToken(tokenString string) (*Claims, error) {
	secret, _, err := snapshotState()
	if err != nil {
		return nil, err
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("不支持的签名算法: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// RefreshToken 刷新 Token。
func RefreshToken(tokenString string) (string, string, string, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return "", "", "", err
	}

	return GenerateTokenPair(claims.UserID, claims.Username, false)
}

// GetExpireTime 获取过期时间（小时）。
func GetExpireTime() int {
	jwtMu.RLock()
	defer jwtMu.RUnlock()

	if !inited {
		return getDefaultConfig().ExpireTime
	}
	return jwtConfig.ExpireTime
}

// MustBeReady 检查 JWT 是否可用。
func MustBeReady() error {
	if _, _, err := snapshotState(); err != nil {
		return fmt.Errorf("JWT 组件不可用: %w", err)
	}
	return nil
}

// Ready 检查 JWT 组件是否可用。
func Ready() error {
	return MustBeReady()
}

func snapshotState() ([]byte, Config, error) {
	jwtMu.RLock()
	defer jwtMu.RUnlock()

	if !inited || len(jwtSecret) == 0 {
		return nil, Config{}, errors.New("JWT 未初始化")
	}

	secret := make([]byte, len(jwtSecret))
	copy(secret, jwtSecret)
	return secret, jwtConfig, nil
}
