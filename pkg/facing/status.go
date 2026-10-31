package facing

import "errors"

type Statused interface {
	HTTPStatus() int
}

type statusError struct {
	err    error
	status int
}

func (e *statusError) Error() string   { return e.err.Error() }
func (e *statusError) Unwrap() error   { return e.err }
func (e *statusError) HTTPStatus() int { return e.status }

func (e *statusError) Is(target error) bool { return errors.Is(e.err, target) }

func WithStatus(err error, status int) error {
	if err == nil || status <= 0 {
		return err
	}
	return &statusError{err: err, status: status}
}

func StatusOf(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var s Statused
	if errors.As(err, &s) {
		return s.HTTPStatus(), true
	}
	return 0, false
}
