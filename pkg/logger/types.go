package logger

import (
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultBaseDir = "public/logs"
	defaultScene   = "default"
	defaultLogFile = "app"
	dateFormat     = "2006-01-02"
)

type runtimeConfig struct {
	baseDir          string
	level            zapcore.Level
	sceneLevels      map[string]zapcore.Level
	sampleEnabled    bool
	sampleInitial    int
	sampleThereafter int
	maxAge           int
}

// Entry 场景日志入口。
type Entry struct {
	scene            string
	autoSceneByLevel bool
	fields           []zap.Field
	initErr          error
}

var (
	mu           sync.RWMutex
	inited       bool
	cfg          runtimeConfig
	sceneLoggers sync.Map // key: scene, value: *zap.Logger
	sceneClosers sync.Map // key: scene, value: io.Closer
)
