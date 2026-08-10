// Package captcha 验证码组件，提供验证码的生成、存储与验证能力。
//
// 支持三种验证码类型：
//   - TypeDigit：纯数字（默认）
//   - TypeAlphanumeric：字母数字混合（去掉了易混淆字符 O/0/1/l/I）
//   - TypeMath：数学运算（加减乘）
//
// 存储默认使用 MemoryStore（进程内内存），启动后台协程每 5 分钟清理过期验证码。
// 可自行实现 Store 接口替换为 Redis 存储。
//
// 使用方式：
//
//	captcha.Init(&captcha.Config{Length: 4, ExpireTime: 2 * time.Minute})
//	id, code := captcha.Get().Generate()
//	ok := captcha.Get().Verify(id, code, true)
package captcha

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// CaptchaType 验证码类型
type CaptchaType string

const (
	TypeDigit        CaptchaType = "digit"        // 纯数字
	TypeAlphanumeric CaptchaType = "alphanumeric" // 字母数字混合
	TypeMath         CaptchaType = "math"         // 数学运算
)

// Captcha 验证码信息
type Captcha struct {
	Code      string
	ExpiresAt time.Time
}

// Store 验证码存储接口
// 可自行实现用 Redis 替换 MemoryStore。
type Store interface {
	Set(id string, captcha *Captcha)
	Get(id string) *Captcha
	Delete(id string)
}

// MemoryStore 基于内存的验证码存储。
// 启动后台协程每 5 分钟清理过期数据。
type MemoryStore struct {
	mu       sync.RWMutex
	captchas map[string]*Captcha
}

// NewMemoryStore 创建内存存储并启动过期清理协程。
func NewMemoryStore() *MemoryStore {
	store := &MemoryStore{
		captchas: make(map[string]*Captcha),
	}
	go store.cleanup()
	return store
}

func (s *MemoryStore) Set(id string, captcha *Captcha) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captchas[id] = captcha
}

func (s *MemoryStore) Get(id string) *Captcha {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.captchas[id]
}

func (s *MemoryStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.captchas, id)
}

func (s *MemoryStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, captcha := range s.captchas {
			if captcha.ExpiresAt.Before(now) {
				delete(s.captchas, id)
			}
		}
		s.mu.Unlock()
	}
}

// Config 验证码配置
type Config struct {
	Length     int           // 验证码长度
	ExpireTime time.Duration // 过期时间
	Width      int           // 图片宽度（预留）
	Height     int           // 图片高度（预留）
}

// CaptchaService 验证码服务
type CaptchaService struct {
	config *Config
	store  Store
}

var (
	captchaService *CaptchaService
	mu             sync.Mutex
)

// Init 初始化验证码服务。
// cfg 为 nil 时使用默认值（Length=6, ExpireTime=1min, Width=120, Height=40）。
func Init(cfg *Config) {
	mu.Lock()
	defer mu.Unlock()

	if captchaService != nil {
		return
	}

	if cfg == nil {
		cfg = &Config{
			Length:     6,
			ExpireTime: 1 * time.Minute,
			Width:      120,
			Height:     40,
		}
	}
	captchaService = &CaptchaService{
		config: cfg,
		store:  NewMemoryStore(),
	}
}

// Get 获取验证码服务单例。
// 未初始化时自动用默认值初始化。
func Get() *CaptchaService {
	if captchaService == nil {
		Init(nil)
	}
	return captchaService
}

// GenerateByType 按类型生成验证码。
// 返回 id（用于验证时的标识）、display（展示给用户的文本）、answer（正确答案）。
func (s *CaptchaService) GenerateByType(captchaType CaptchaType) (id, display, answer string) {
	switch captchaType {
	case TypeAlphanumeric:
		id, code := s.GenerateAlphanumeric()
		return id, code, code
	case TypeMath:
		return s.GenerateMath()
	default:
		id, code := s.Generate()
		return id, code, code
	}
}

// Generate 生成纯数字验证码。
func (s *CaptchaService) Generate() (id string, code string) {
	idBytes := make([]byte, 16)
	rand.Read(idBytes)
	id = fmt.Sprintf("%x", idBytes)

	code = s.generateDigitCode(s.config.Length)

	s.store.Set(id, &Captcha{
		Code:      code,
		ExpiresAt: time.Now().Add(s.config.ExpireTime),
	})

	return id, code
}

// GenerateWithPrefix 生成带业务前缀的验证码，用于区分不同业务场景。
func (s *CaptchaService) GenerateWithPrefix(prefix string) (id string, code string) {
	idBytes := make([]byte, 16)
	rand.Read(idBytes)
	id = prefix + "_" + fmt.Sprintf("%x", idBytes)

	code = s.generateDigitCode(s.config.Length)

	s.store.Set(id, &Captcha{
		Code:      code,
		ExpiresAt: time.Now().Add(s.config.ExpireTime),
	})

	return id, code
}

// GenerateAlphanumeric 生成字母数字混合验证码。
// 已排除易混淆字符：O/0/1/l/I。
func (s *CaptchaService) GenerateAlphanumeric() (id string, code string) {
	idBytes := make([]byte, 16)
	rand.Read(idBytes)
	id = fmt.Sprintf("%x", idBytes)

	chars := "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789"
	codeBytes := make([]byte, s.config.Length)
	for i := 0; i < s.config.Length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		codeBytes[i] = chars[n.Int64()]
	}
	code = string(codeBytes)

	s.store.Set(id, &Captcha{
		Code:      code,
		ExpiresAt: time.Now().Add(s.config.ExpireTime),
	})

	return id, code
}

// GenerateMath 生成数学运算验证码。
// 返回 id、表达式（如 "3 + 5 = ?"）、答案（如 "8"）。
// 减法保证大减小，乘法操作数范围 1-10。
func (s *CaptchaService) GenerateMath() (id, expression, answer string) {
	idBytes := make([]byte, 16)
	rand.Read(idBytes)
	id = fmt.Sprintf("%x", idBytes)

	a := randInt(1, 20)
	b := randInt(1, 20)

	ops := []string{"+", "-", "×"}
	opIdx := randInt(0, len(ops))
	op := ops[opIdx]

	var result int
	switch op {
	case "+":
		result = a + b
	case "-":
		if a < b {
			a, b = b, a
		}
		result = a - b
	case "×":
		a = randInt(1, 10)
		b = randInt(1, 10)
		result = a * b
	}

	expression = fmt.Sprintf("%d %s %d = ?", a, op, b)
	answer = fmt.Sprintf("%d", result)

	s.store.Set(id, &Captcha{
		Code:      answer,
		ExpiresAt: time.Now().Add(s.config.ExpireTime),
	})

	return id, expression, answer
}

// Verify 验证验证码。
// clear=true 表示验证成功后清除，实现一次性验证。
func (s *CaptchaService) Verify(id, code string, clear bool) bool {
	captcha := s.store.Get(id)
	if captcha == nil {
		return false
	}

	if captcha.ExpiresAt.Before(time.Now()) {
		s.store.Delete(id)
		return false
	}

	if captcha.Code != code {
		return false
	}

	if clear {
		s.store.Delete(id)
	}

	return true
}

// Close 关闭验证码服务，清空运行时状态。
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	captchaService = nil
	return nil
}

func (s *CaptchaService) generateDigitCode(length int) string {
	digits := "0123456789"
	code := make([]byte, length)
	for i := 0; i < length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		code[i] = digits[n.Int64()]
	}
	return string(code)
}

func randInt(min, max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	return int(n.Int64()) + min
}
