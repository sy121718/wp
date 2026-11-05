package facing

type Error struct {
	key      string
	fallback string
}

func New(key string) *Error {
	return &Error{key: key}
}

// NewMessage 构造带兜底文案的错误事实，供共享失败出口按当前语言取词。
func NewMessage(key, fallback string) *Error {
	return &Error{key: key, fallback: fallback}
}

func (e *Error) Error() string { return e.key }

func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}
	return target.Error() == e.key
}

func (e *Error) Facing() (key, fallback string) { return e.key, e.fallback }

type Labeled struct {
	Label string
	Msg   Msg
}
