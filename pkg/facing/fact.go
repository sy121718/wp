package facing

import (
	"errors"
	"regexp"
	"strings"
)

var businessErrKey = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.[a-zA-Z0-9_]+(\.[a-zA-Z0-9_]+)*(\|.*)?$`)

var businessErrConstant = regexp.MustCompile(`^(Err|Msg)[A-Z][A-Za-z0-9]*(\|.*)?$`)

func IsBusinessKey(msg string) bool {
	return businessErrKey.MatchString(msg) || businessErrConstant.MatchString(msg)
}

// IsBusinessError 复用错误事实判定，显式声明的 key 不依赖字符串形状。
func IsBusinessError(err error) bool {
	fact, ok := FactOf(err)
	return ok && fact.Business
}

type Fact struct {
	Key      string
	Fallback string
	Tail     string
	Business bool
}

type Source interface {
	Facing() (key, fallback string)
}

func FactOf(err error) (Fact, bool) {
	if err == nil {
		return Fact{}, false
	}

	if f, ok := selfFacingFact(err); ok {
		return f, true
	}
	raw := strings.TrimSpace(err.Error())

	if IsBusinessKey(raw) {
		return Fact{Key: raw, Fallback: raw, Business: true}, true
	}

	idx := strings.IndexByte(raw, ':')
	if idx <= 0 {
		return Fact{}, false
	}
	key, tail := strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+1:])
	if key == "" || !IsBusinessKey(key) {
		return Fact{}, false
	}
	return Fact{Key: key, Fallback: key, Tail: tail, Business: true}, true
}

func selfFacingFact(err error) (Fact, bool) {
	var src Source
	if !errors.As(err, &src) {
		return Fact{}, false
	}
	key, fallback := src.Facing()
	if strings.TrimSpace(key) == "" {
		return Fact{}, false
	}
	return Fact{
		Key:      key,
		Fallback: fallback,
		Tail:     tailAfterKey(err.Error(), key),
		Business: true,
	}, true
}

func tailAfterKey(raw, key string) string {
	// 外层包装上下文不是业务明细，不让内部诊断混进对外消息。
	if !strings.HasPrefix(strings.TrimSpace(raw), key) {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), key))
	return strings.TrimSpace(strings.TrimPrefix(rest, ":"))
}
