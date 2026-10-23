package logger

import (
	"context"

	"go.uber.org/zap"
)

// With 使用默认场景并添加单个日志字段。
func With(key string, value any) *Entry {
	return newAutoEntry().With(key, value)
}

// WithFields 使用默认场景并批量添加日志字段。
func WithFields(fields map[string]any) *Entry {
	return newAutoEntry().WithFields(fields)
}

// Debug 使用默认场景记录 debug 级别日志。
func Debug(msg string) {
	newAutoEntry().Debug(msg)
}

// Info 使用默认场景记录 info 级别日志。
func Info(msg string) {
	newAutoEntry().Info(msg)
}

// Warn 使用默认场景记录 warn 级别日志。
func Warn(msg string) {
	newAutoEntry().Warn(msg)
}

// Error 使用默认场景记录 error 级别日志。
func Error(err error, msg string) {
	newAutoEntry().Error(err, msg)
}

// DPanic 使用默认场景记录 dpanic 级别日志。
func DPanic(msg string) {
	newAutoEntry().DPanic(msg)
}

// Panic 使用默认场景记录 panic 级别日志。
func Panic(msg string) {
	newAutoEntry().Panic(msg)
}

// Fatal 使用默认场景记录 fatal 级别日志。
func Fatal(msg string) {
	newAutoEntry().Fatal(msg)
}

// Scene 返回指定场景的日志入口。
func Scene(scene string) *Entry {
	return &Entry{
		scene:            normalizeScene(scene),
		autoSceneByLevel: false,
		fields:           make([]zap.Field, 0, 8),
		initErr:          ensureInited(),
	}
}

// ─── 请求关联（request id）───
//
// 为什么由 logger 侧持有「从 ctx 取 id」这件事，而不是让每个调用点自己 With：
// 全仓 34 个 scene、数百个日志调用点，要求每处都记得带同一个字段，漏掉的那个永远
// 发现不了（日志少一个字段不会报错）。从 ctx 取则只有一种缺口 —— 调用方忘了传 ctx，
// 而 Ctx 系列把 ctx 做成第一个参数，编译器会替我们盯住它。
//
// 与 RequestID 的关系：id 由 internal/middleware/builtin 的中间件生成并写进
// context，这里是**唯一的读取点**。两头都只认这一个键，中间件换实现也不影响日志。

// requestIDKey request_id 在 context 里的键（未导出的私有类型，避免与其它包的键相撞）。
type requestIDKey struct{}

// WithRequestID 把 request id 放进 context。
//
// 只在请求入口调用一次（中间件）。传空串等同于不加。
func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil || id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFrom 从 context 取 request id（未设置返回空串）。
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// Ctx 返回绑定到 ctx 的默认场景日志入口：ctx 里有 request id 就自动带上。
//
// 用法：logger.Ctx(ctx).With("path", p).Error(err, "…")。
// 与 Scene(ctx, …) 的区别只是场景：这个是默认场景。
func Ctx(ctx context.Context) *Entry {
	return newAutoEntry().withCtx(ctx)
}

// SceneCtx 返回绑定到 ctx 的指定场景日志入口（ctx 有 request id 就自动带上）。
//
// 这是 HTTP 侧应当优先使用的形态：既分类（scene）又关联（request_id）。
func SceneCtx(scene string, ctx context.Context) *Entry {
	return Scene(scene).withCtx(ctx)
}

// withCtx 把 ctx 里的 request id 附加为一个字段（没有就不加）。
func (e *Entry) withCtx(ctx context.Context) *Entry {
	if id := RequestIDFrom(ctx); id != "" {
		e.fields = append(e.fields, zap.String("request_id", id))
	}
	return e
}
