package database

import (
	"context"
	"go_wp/pkg/logger"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

const defaultSQLSlowThreshold = 200 * time.Millisecond

type sceneGormLogger struct {
	inner         gormlogger.Interface
	scene         string
	slowThreshold time.Duration
}

func newSceneGormLogger(inner gormlogger.Interface, scene string, slowThreshold time.Duration) gormlogger.Interface {
	if scene == "" {
		return inner
	}
	if slowThreshold <= 0 {
		slowThreshold = defaultSQLSlowThreshold
	}
	return &sceneGormLogger{
		inner:         inner,
		scene:         scene,
		slowThreshold: slowThreshold,
	}
}

func (l *sceneGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return &sceneGormLogger{
		inner:         l.inner.LogMode(level),
		scene:         l.scene,
		slowThreshold: l.slowThreshold,
	}
}

func (l *sceneGormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	l.inner.Info(ctx, msg, data...)
}

func (l *sceneGormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	l.inner.Warn(ctx, msg, data...)
}

func (l *sceneGormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	l.inner.Error(ctx, msg, data...)
}

func (l *sceneGormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.inner.Trace(ctx, begin, fc, err)

	sql, rows := fc()
	elapsed := time.Since(begin)
	fields := map[string]interface{}{
		"sql":        sql,
		"rows":       rows,
		"elapsed_ms": elapsed.Milliseconds(),
	}

	entry := logger.Scene(l.scene).WithFields(fields)
	if err != nil {
		entry.Error(err, "sql execution error")
		return
	}

	if elapsed >= l.slowThreshold {
		entry.Warn("slow sql")
		return
	}

	entry.Info("sql query")
}
