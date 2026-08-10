package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dailyRotateWriter 按日期轮转的日志写入器。
// 每天产生一个新文件，文件名格式：{prefix}-2006-01-02.log。
// maxAge > 0 时自动清理过期文件。
type dailyRotateWriter struct {
	mu     sync.Mutex
	dir    string
	prefix string // 文件前缀，例如 "app"
	maxAge int    // 最大保留天数，0 表示不清理
	keep   int    // 最大保留文件数，0 表示不限制

	file *os.File
	date string // 当前文件对应的日期 "2006-01-02"
}

// newDailyRotateWriter 创建按日期轮转的写入器。
func newDailyRotateWriter(dir, prefix string, maxAge int) (*dailyRotateWriter, error) {
	w := &dailyRotateWriter{
		dir:    dir,
		prefix: prefix,
		maxAge: maxAge,
	}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	w.cleanup()
	return w, nil
}

// Write 写入日志，跨天时自动切换文件。
func (w *dailyRotateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format(dateFormat)
	if w.date != today {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	if w.file == nil {
		return 0, fmt.Errorf("日志文件未打开")
	}
	return w.file.Write(p)
}

// Close 关闭当前日志文件。
func (w *dailyRotateWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeFile()
}

// rotate 关闭旧文件，按当天日期打开新文件。
func (w *dailyRotateWriter) rotate() error {
	if err := w.closeFile(); err != nil {
		return err
	}

	w.date = time.Now().Format(dateFormat)
	filename := fmt.Sprintf("%s-%s.log", w.prefix, w.date)
	fullPath := filepath.Join(w.dir, filename)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败, path=%s: %w", fullPath, err)
	}
	w.file = f
	return nil
}

// closeFile 关闭当前打开的文件。
func (w *dailyRotateWriter) closeFile() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// cleanup 清理超过 maxAge 天的旧日志文件。
func (w *dailyRotateWriter) cleanup() {
	if w.maxAge <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -w.maxAge)
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(w.dir, entry.Name()))
		}
	}
}