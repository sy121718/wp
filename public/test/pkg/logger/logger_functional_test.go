package logger_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go_wp/pkg/logger"

	"github.com/spf13/viper"
)

func TestLoggerInitReturnsErrorWhenBaseDirIsFile(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "not_dir")
	if err := os.WriteFile(basePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	cfg := viper.New()
	cfg.Set("log.base_dir", basePath)
	if err := logger.Init(cfg); err == nil {
		t.Fatalf("日志初始化应当失败，但返回 nil")
	}
}

func TestLoggerConcurrentWriteAndSync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "go_wp-logger-*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	cfg := viper.New()
	cfg.Set("log.base_dir", tmpDir)
	cfg.Set("log.level", "debug")

	if err := logger.Init(cfg); err != nil {
		t.Fatalf("日志初始化失败: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				logger.Scene("concurrency").With("worker", idx).With("seq", j).Info("并发日志测试")
			}
		}()
	}
	wg.Wait()

	if err := logger.Close(); err != nil {
		t.Fatalf("日志 Sync 失败: %v", err)
	}

	content := readSceneLog(t, tmpDir, "concurrency")
	if len(content) == 0 {
		t.Fatalf("日志文件内容为空")
	}
}

func TestLoggerSceneLevelOverride(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := viper.New()
	cfg.Set("log.base_dir", tmpDir)
	cfg.Set("log.level", "info")
	cfg.Set("log.scene_levels.sql", "error")

	if err := logger.Init(cfg); err != nil {
		t.Fatalf("日志初始化失败: %v", err)
	}

	logger.Scene("sql").Info("这条不应写入")
	logger.Scene("sql").Error(nil, "这条应写入")

	if err := logger.Close(); err != nil {
		t.Fatalf("日志关闭失败: %v", err)
	}

	content := readSceneLog(t, tmpDir, "sql")

	text := string(content)
	if strings.Contains(text, "这条不应写入") {
		t.Fatalf("sql 场景级别覆盖未生效")
	}
	if !strings.Contains(text, "这条应写入") {
		t.Fatalf("sql error 日志未写入")
	}
}

func TestLoggerSamplingReducesRepeatedLogs(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := viper.New()
	cfg.Set("log.base_dir", tmpDir)
	cfg.Set("log.level", "info")
	cfg.Set("log.sample.enabled", true)
	cfg.Set("log.sample.initial", 1)
	cfg.Set("log.sample.thereafter", 100)

	if err := logger.Init(cfg); err != nil {
		t.Fatalf("日志初始化失败: %v", err)
	}

	for i := 0; i < 5; i++ {
		logger.Scene("http").Info("重复日志")
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("日志关闭失败: %v", err)
	}

	content := readSceneLog(t, tmpDir, "http")

	count := bytes.Count(content, []byte("重复日志"))
	if count >= 5 {
		t.Fatalf("日志采样未生效: got=%d", count)
	}
}

func readSceneLog(t *testing.T, baseDir, scene string) []byte {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(baseDir, scene, "app-*.log"))
	if err != nil {
		t.Fatalf("匹配日志文件失败: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("日志文件数量不正确: got=%d want=1", len(paths))
	}

	content, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}
	return content
}
