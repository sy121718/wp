package logger

import "go.uber.org/zap"

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
