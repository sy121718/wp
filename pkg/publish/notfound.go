package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const NotFoundFileName = "404.html"

var siteRootFileNames = map[string]struct{}{
	"sitemap.xml":    {},
	"robots.txt":     {},
	"feed.xml":       {},
	NotFoundFileName: {},
}

func IsSiteRootFile(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || strings.Contains(rel, "/") {
		return false
	}
	_, ok := siteRootFileNames[rel]
	return ok
}

func NotFoundPagePath(activeRoot string) string {
	return filepath.Join(activeRoot, NotFoundFileName)
}

func ReadNotFoundPage(activeRoot string) (body []byte, ok bool) {
	if strings.TrimSpace(activeRoot) == "" {
		return nil, false
	}
	data, err := os.ReadFile(NotFoundPagePath(activeRoot))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

func RemoveNotFoundPage(activeRoot string) error {
	if strings.TrimSpace(activeRoot) == "" {
		return nil
	}
	if err := os.Remove(NotFoundPagePath(activeRoot)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func SyncNotFoundPage(activeRoot, html string) error {
	if strings.TrimSpace(activeRoot) == "" {
		return nil
	}
	if strings.TrimSpace(html) == "" {
		return RemoveNotFoundPage(activeRoot)
	}
	if _, serr := os.Stat(activeRoot); serr != nil {
		if os.IsNotExist(serr) {
			return nil
		}
		return serr
	}
	target := NotFoundPagePath(activeRoot)
	tmp := target + ".tmp"
	if werr := os.WriteFile(tmp, []byte(html), 0o644); werr != nil {
		return fmt.Errorf("写入自定义 404 页失败: %w", werr)
	}
	if rerr := os.Rename(tmp, target); rerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("落盘自定义 404 页失败: %w", rerr)
	}
	return nil
}
