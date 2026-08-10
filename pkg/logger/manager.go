package logger

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Init 初始化 zap 日志运行时配置。
func Init(v *viper.Viper) error {
	next := runtimeConfig{
		baseDir:          defaultBaseDir,
		level:            zapcore.InfoLevel,
		sceneLevels:      map[string]zapcore.Level{},
		sampleEnabled:    false,
		sampleInitial:    100,
		sampleThereafter: 100,
		maxAge:           30,
	}

	if v != nil {
		if baseDir := strings.TrimSpace(v.GetString("log.base_dir")); baseDir != "" {
			next.baseDir = baseDir
		}
		next.level = parseLevel(v.GetString("log.level"))
		if age := v.GetInt("log.max_age"); age > 0 {
			next.maxAge = age
		}
		next.sampleEnabled = v.GetBool("log.sample.enabled")
		if initial := v.GetInt("log.sample.initial"); initial > 0 {
			next.sampleInitial = initial
		}
		if thereafter := v.GetInt("log.sample.thereafter"); thereafter > 0 {
			next.sampleThereafter = thereafter
		}
		if rawLevels := v.GetStringMapString("log.scene_levels"); len(rawLevels) > 0 {
			next.sceneLevels = make(map[string]zapcore.Level, len(rawLevels))
			for scene, levelText := range rawLevels {
				next.sceneLevels[normalizeScene(scene)] = parseLevel(levelText)
			}
		}
	}

	next.baseDir = filepath.Clean(next.baseDir)
	if err := os.MkdirAll(next.baseDir, 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	mu.Lock()
	cfg = next
	inited = true
	mu.Unlock()

	if err := clearSceneLoggers(); err != nil {
		return fmt.Errorf("清理旧 logger 失败: %w", err)
	}
	return nil
}

// Close 刷新并落盘所有场景 logger。
func Close() error {
	mu.Lock()
	inited = false
	cfg = runtimeConfig{}
	mu.Unlock()
	return clearSceneLoggers()
}

func getSceneLogger(scene string) (*zap.Logger, error) {
	scene = normalizeScene(scene)

	if value, ok := sceneLoggers.Load(scene); ok {
		if lg, castOK := value.(*zap.Logger); castOK {
			return lg, nil
		}
	}

	lg, closer, err := newSceneLogger(scene)
	if err != nil {
		return nil, err
	}

	actual, loaded := sceneLoggers.LoadOrStore(scene, lg)
	if loaded {
		if err := syncOneLogger(scene, lg); err != nil {
			log.Printf("关闭重复创建的 logger 失败, scene=%s, err=%v", scene, err)
		}
		if closer != nil {
			if err := closer.Close(); err != nil {
				log.Printf("关闭重复创建的 logger writer 失败, scene=%s, err=%v", scene, err)
			}
		}
		if existing, ok := actual.(*zap.Logger); ok {
			return existing, nil
		}
	} else if closer != nil {
		sceneClosers.Store(scene, closer)
	}
	return lg, nil
}

func clearSceneLoggers() error {
	var syncErr error
	sceneLoggers.Range(func(key, value any) bool {
		if lg, ok := value.(*zap.Logger); ok {
			err := syncOneLogger(key, lg)
			syncErr = errors.Join(syncErr, err)
		}
		if closer, ok := sceneClosers.Load(key); ok {
			if c, castOK := closer.(io.Closer); castOK {
				err := c.Close()
				syncErr = errors.Join(syncErr, err)
			}
			sceneClosers.Delete(key)
		}
		sceneLoggers.Delete(key)
		return true
	})
	return syncErr
}

func ensureInited() error {
	mu.RLock()
	currentInited := inited
	mu.RUnlock()
	if currentInited {
		return nil
	}

	return fmt.Errorf("logger 未初始化，请先调用 logger.Init()")
}

// Ready 检查 logger 组件是否已显式初始化。
func Ready() error {
	return ensureInited()
}

func syncOneLogger(scene any, lg *zap.Logger) error {
	if lg == nil {
		return nil
	}
	if err := lg.Sync(); err != nil {
		return fmt.Errorf("刷新场景日志失败, scene=%v: %w", scene, err)
	}
	return nil
}
