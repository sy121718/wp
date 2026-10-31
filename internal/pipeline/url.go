package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go_wp/pkg/pathkit"
)

var reservedPrefixes = []string{
	"/admin",
	"/api",
	"/_fragments",
	"/assets",
	"/objects",
}

func NormalizeURL(raw string) (string, error) {
	p, err := pathkit.NormalizeRoutePath(raw)
	if err != nil {
		return "", err
	}
	for _, rp := range reservedPrefixes {
		if p == rp || strings.HasPrefix(p, rp+"/") {
			return "", fmt.Errorf("路径 %q 为系统保留路径（%s）", p, rp)
		}
	}
	return p, nil
}

func CleanSiteRel(raw string) (rel string, ok bool) {
	rel = strings.Trim(raw, "/")
	if rel == "" {
		return "", true
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
		return "", false
	}
	return clean, true
}

func ResolveActiveEntry(root, rel string) (entry string, ok bool) {
	clean := strings.Trim(strings.TrimSpace(rel), "/")

	direct := clean
	if direct == "" {
		direct = "index"
	}
	if isRegularFile(filepath.Join(root, filepath.FromSlash(direct), "index.html")) {
		return direct, true
	}

	base, found := strings.CutSuffix(clean, "/index.html")
	if !found || base == "" {
		return "", false
	}
	link := filepath.Join(root, filepath.FromSlash(base))
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(link), target))
	if !isRegularFile(filepath.Join(dir, "index.html")) {
		return "", false
	}
	return base, true
}

func isRegularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
