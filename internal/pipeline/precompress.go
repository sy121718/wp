package pipeline

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"
)

const PrecompressedSuffix = ".gz"

const PrecompressMinSize = 1024

const gzipOSUnknown = 255

var precompressibleExts = map[string]struct{}{
	".html":        {},
	".htm":         {},
	".css":         {},
	".js":          {},
	".mjs":         {},
	".json":        {},
	".xml":         {},
	".svg":         {},
	".txt":         {},
	".webmanifest": {},
	".map":         {},
}

func IsPrecompressible(name string, size int) bool {
	if size < PrecompressMinSize {
		return false
	}
	_, ok := precompressibleExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

func GzipDeterministic(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("gzip 压缩器初始化失败: %w", err)
	}
	zw.ModTime = time.Unix(0, 0)
	zw.OS = gzipOSUnknown
	if _, err = zw.Write(data); err != nil {
		return nil, fmt.Errorf("gzip 压缩写入失败: %w", err)
	}
	if err = zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip 压缩收尾失败: %w", err)
	}
	return buf.Bytes(), nil
}

func PrecompressEntries(entries map[string][]byte) error {

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	for _, name := range names {
		data := entries[name]
		if !IsPrecompressible(name, len(data)) {
			continue
		}
		gz, err := GzipDeterministic(data)
		if err != nil {

			return fmt.Errorf("预压缩 %s 失败: %w", name, err)
		}
		entries[name+PrecompressedSuffix] = gz
	}
	return nil
}

func AssetContentType(path string) string {
	name := strings.ToLower(path)
	ctype := mime.TypeByExtension(filepath.Ext(name))
	if strings.HasSuffix(name, ".html") {

		ctype = "text/html; charset=utf-8"
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return ctype
}
