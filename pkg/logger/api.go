package logger

import (
	"context"

	"go.uber.org/zap"
)

func With(key string, value any) *Entry {
	return newAutoEntry().With(key, value)
}

func WithFields(fields map[string]any) *Entry {
	return newAutoEntry().WithFields(fields)
}

func Debug(msg string) {
	newAutoEntry().Debug(msg)
}

func Info(msg string) {
	newAutoEntry().Info(msg)
}

func Warn(msg string) {
	newAutoEntry().Warn(msg)
}

func Error(err error, msg string) {
	newAutoEntry().Error(err, msg)
}

func DPanic(msg string) {
	newAutoEntry().DPanic(msg)
}

func Panic(msg string) {
	newAutoEntry().Panic(msg)
}

func Fatal(msg string) {
	newAutoEntry().Fatal(msg)
}

func Scene(scene string) *Entry {
	return &Entry{
		scene:            normalizeScene(scene),
		autoSceneByLevel: false,
		fields:           make([]zap.Field, 0, 8),
		initErr:          ensureInited(),
	}
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil || id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

func Ctx(ctx context.Context) *Entry {
	return newAutoEntry().withCtx(ctx)
}

func SceneCtx(scene string, ctx context.Context) *Entry {
	return Scene(scene).withCtx(ctx)
}

func (e *Entry) withCtx(ctx context.Context) *Entry {
	if id := RequestIDFrom(ctx); id != "" {
		e.fields = append(e.fields, zap.String("request_id", id))
	}
	return e
}
