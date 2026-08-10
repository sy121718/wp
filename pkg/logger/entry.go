package logger

import (
	"fmt"
	"log"
	"strings"

	"go.uber.org/zap"
)

// With 添加单个日志字段。
func (e *Entry) With(key string, value any) *Entry {
	if strings.TrimSpace(key) == "" {
		return e
	}
	e.fields = append(e.fields, zap.Any(key, value))
	return e
}

// WithFields 批量添加日志字段。
func (e *Entry) WithFields(fields map[string]any) *Entry {
	for k, v := range fields {
		if strings.TrimSpace(k) == "" {
			continue
		}
		e.fields = append(e.fields, zap.Any(k, v))
	}
	return e
}

// Debug 记录 debug 级别日志。
func (e *Entry) Debug(msg string) {
	e.write("debug", msg, nil)
}

// Info 记录 info 级别日志。
func (e *Entry) Info(msg string) {
	e.write("info", msg, nil)
}

// Warn 记录 warn 级别日志。
func (e *Entry) Warn(msg string) {
	e.write("warn", msg, nil)
}

// Error 记录 error 级别日志。
func (e *Entry) Error(err error, msg string) {
	e.write("error", msg, err)
}

// DPanic 记录 dpanic 级别日志。
func (e *Entry) DPanic(msg string) {
	e.write("dpanic", msg, nil)
}

// Panic 记录 panic 级别日志。
func (e *Entry) Panic(msg string) {
	e.write("panic", msg, nil)
}

// Fatal 记录 fatal 级别日志。
func (e *Entry) Fatal(msg string) {
	e.write("fatal", msg, nil)
}

func (e *Entry) write(level string, msg string, err error) {
	logger, loggerErr := e.getLogger(level)
	if loggerErr != nil {
		log.Printf("logger 写入失败, level=%s, scene=%s, err=%v, msg=%s", level, e.sceneForLevel(level), loggerErr, strings.TrimSpace(msg))
		return
	}

	fields := e.fields
	if err != nil {
		fields = append(append([]zap.Field{}, fields...), zap.Error(err))
	}

	trimmedMsg := strings.TrimSpace(msg)
	switch level {
	case "debug":
		logger.Debug(trimmedMsg, fields...)
	case "info":
		logger.Info(trimmedMsg, fields...)
	case "warn":
		logger.Warn(trimmedMsg, fields...)
	case "error":
		logger.Error(trimmedMsg, fields...)
	case "dpanic":
		logger.DPanic(trimmedMsg, fields...)
	case "panic":
		logger.Panic(trimmedMsg, fields...)
	case "fatal":
		logger.Fatal(trimmedMsg, fields...)
	default:
		log.Printf("logger 写入失败, 未知日志级别=%s, msg=%s", level, trimmedMsg)
	}
}

func (e *Entry) getLogger(levelScene string) (*zap.Logger, error) {
	if e.initErr != nil {
		return nil, e.initErr
	}

	logger, err := getSceneLogger(e.sceneForLevel(levelScene))
	if err != nil {
		return nil, fmt.Errorf("获取场景 logger 失败: %w", err)
	}
	return logger, nil
}

func newAutoEntry() *Entry {
	return &Entry{
		scene:            defaultScene,
		autoSceneByLevel: true,
		fields:           make([]zap.Field, 0, 8),
		initErr:          ensureInited(),
	}
}

func (e *Entry) sceneForLevel(levelScene string) string {
	if e.autoSceneByLevel {
		return normalizeScene(levelScene)
	}
	return e.scene
}
