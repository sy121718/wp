package facing

type Error struct {
	key      string
	fallback string
}

func New(key, fallback string) *Error {
	if fallback == "" {
		fallback = key
	}
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
