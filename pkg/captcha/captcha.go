package captcha

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"
)

type CaptchaType string

const (
	TypeDigit        CaptchaType = "digit"
	TypeAlphanumeric CaptchaType = "alphanumeric"
	TypeMath         CaptchaType = "math"
)

type Captcha struct {
	Code      string
	ExpiresAt time.Time
}

type Store interface {
	Set(id string, captcha *Captcha)
	Get(id string) *Captcha
	Delete(id string)
}

type MemoryStore struct {
	mu         sync.RWMutex
	captchas   map[string]*Captcha
	maxEntries int
	stopCh     chan struct{}
	stopOnce   sync.Once
}

const defaultMaxEntries = 10000

func NewMemoryStore() *MemoryStore {
	store := &MemoryStore{
		captchas:   make(map[string]*Captcha),
		maxEntries: defaultMaxEntries,
		stopCh:     make(chan struct{}),
	}
	go store.cleanup()
	return store
}

func (s *MemoryStore) Set(id string, captcha *Captcha) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.captchas[id]; !exists && len(s.captchas) >= s.maxEntries {
		now := time.Now()
		for k, v := range s.captchas {
			if v.ExpiresAt.Before(now) {
				delete(s.captchas, k)
			}
		}
		if len(s.captchas) >= s.maxEntries {
			return
		}
	}
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
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
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
}

func (s *MemoryStore) Close() error {
	s.stopOnce.Do(func() { close(s.stopCh) })
	return nil
}

type Config struct {
	Length     int
	ExpireTime time.Duration
	Width      int
	Height     int
}

type CaptchaService struct {
	config *Config
	store  Store
}

var (
	captchaService *CaptchaService
	mu             sync.Mutex
)

func Init(cfg *Config) {
	mu.Lock()
	defer mu.Unlock()

	if captchaService != nil {
		return
	}

	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Length <= 0 {
		cfg.Length = 6
	}
	if cfg.ExpireTime <= 0 {
		cfg.ExpireTime = 1 * time.Minute
	}
	if cfg.Width <= 0 {
		cfg.Width = 120
	}
	if cfg.Height <= 0 {
		cfg.Height = 40
	}
	captchaService = &CaptchaService{
		config: cfg,
		store:  NewMemoryStore(),
	}
}

func Get() *CaptchaService {
	if captchaService == nil {
		Init(nil)
	}
	return captchaService
}

func (s *CaptchaService) ExpireSeconds() int64 {
	if s.config.ExpireTime <= 0 {
		return 0
	}
	return int64(s.config.ExpireTime / time.Second)
}

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

func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if captchaService != nil {
		if ms, ok := captchaService.store.(*MemoryStore); ok {
			_ = ms.Close()
		}
	}
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
