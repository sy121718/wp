package facing

import "errors"

// Statused 错误自带的 HTTP 语义（状态码）。
//
// **谁声明错误，谁声明状态**：一个 key 是「找不到」还是「冲突」，只有声明处知道
// （同一个 ErrNotFound 在不同模块可能就是 404，而「版本冲突」是 409）。
// 出口只读这里给出的值，不去猜错误文案里有没有 "NotFound" —— 按字符串猜分档
// 会在改文案时静默变质：故障仍然发生，但状态码变成了 400 或 500。
type Statused interface {
	HTTPStatus() int
}

// statusError 给既有错误挂一个状态码，其余语义（Error / Unwrap / Is）原样透传。
type statusError struct {
	err    error
	status int
}

func (e *statusError) Error() string   { return e.err.Error() }
func (e *statusError) Unwrap() error   { return e.err }
func (e *statusError) HTTPStatus() int { return e.status }

// Is 让 errors.Is 继续认原来的哨兵：包装之后
// errors.Is(err, moduleenums.ErrX) 仍要成立，否则调用方的分档全部静默落 default。
func (e *statusError) Is(target error) bool { return errors.Is(e.err, target) }

// WithStatus 给错误挂上 HTTP 状态码（status <= 0 或 err == nil 时原样返回）。
//
// 只应在**声明处**使用（模块 enums 的 Err / ErrDetail 工厂），不要在出口再包一次：
// 两处各写一份就是两份判据，而它们的偏差方向最难查 —— 文案按 A 判、状态码按 B 判。
func WithStatus(err error, status int) error {
	if err == nil || status <= 0 {
		return err
	}
	return &statusError{err: err, status: status}
}

// StatusOf 沿 Unwrap 链找第一个声明过状态码的错误。
//
// 复合错误（fmt.Errorf("%w: 明细", ErrX)）同样命中：errors.As 会穿过包装层。
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
