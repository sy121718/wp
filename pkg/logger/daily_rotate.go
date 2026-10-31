package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type dailyRotateWriter struct {
	mu     sync.Mutex
	dir    string
	prefix string
	maxAge int
	keep   int

	file *os.File
	date string
}

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

func (w *dailyRotateWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeFile()
}

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

func (w *dailyRotateWriter) closeFile() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

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
