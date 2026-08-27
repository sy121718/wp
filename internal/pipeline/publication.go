package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 访问面激活状态类型。
const (
	// PublicationPage 激活的是普通 Page Artifact。
	PublicationPage = "page"
	// PublicationRedirect 激活的是重定向产物（redirect.json）。
	PublicationRedirect = "redirect"
	// PublicationNone 该路径未激活。
	PublicationNone = "none"
)

// PublicationState 访问面路由状态（PublicationStore.inspect 返回值）。
type PublicationState struct {
	// Kind page / redirect / none。
	Kind string
	// Locator 当前激活的产物定位器（Kind 为 none 时为空）。
	Locator *Locator
	// Redirect 重定向指令（Kind 为 redirect 时非空）。
	Redirect *RedirectDirective
}

// PublicationStore 访问面 URL 激活契约（docs/03-pipeline.md §5）。
// 只负责 URL → 文件系统状态的原子切换，不构建产物、不查询数据库。
//
// URI 边界：activate/deactivate/inspect 的 path 必须已规范化。
type PublicationStore interface {
	// Activate 原子切换路径到目标产物；新请求只见新 Artifact。
	Activate(path string, loc Locator) error
	// Deactivate 取消激活（幂等：未激活时返回 nil）。
	Deactivate(path string) error
	// Inspect 返回路径当前激活状态（readlink 确认目标）。
	Inspect(path string) (*PublicationState, error)
}

// LocalPublicationStore 本地文件系统实现（docs/03-pipeline.md §5）：
// 符号链接切换策略——activate(path, loc) 为临时 symlink + 原子 rename 覆盖。
//
//	{activeRoot}/about/ → ../../artifacts/{hash}/   （单段路径，两段上溯到产物根）
//
// symlink target 为相对链接：上溯段数 = 路径层数 + ActiveRoot 相对产物根的层数（2），
// 因此多级路径（/products/phone）自动生成 "../../../.." 深度，保证物理可达。
type LocalPublicationStore struct {
	// ActiveRoot 激活目录（如 public/active，位于产物根下两级）。
	ActiveRoot string
}

// Activate 实现 PublicationStore 接口。
func (s *LocalPublicationStore) Activate(path string, loc Locator) error {
	p, err := NormalizeURL(path)
	if err != nil {
		return err
	}
	if loc.Provider == "" || loc.Key == "" {
		return fmt.Errorf("定位器缺少 provider 或 key")
	}

	link := filepath.Join(s.ActiveRoot, relActivePath(p))
	parent := filepath.Dir(link)
	if err = os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	// symlink target 为相对路径：上溯层数 = 链接父目录相对产物根的深度 = 相对段数 + 1
	// （root 下：public/active/{path 段-1}，即 段数 + 1）。
	up := strings.Split(relActivePath(p), "/")
	target := strings.Repeat("../", len(up)+1) + loc.Key
	tmp := link + ".activating-" + locatorHash(loc)

	if err = os.Symlink(target, tmp); err != nil {
		return err
	}
	// 原子覆盖：unix rename 直接替换已有 symlink/文件。
	if err = os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		// 异常中间态：旧位置是目录（非 symlink）时清理后重试一次。
		if fi, serr := os.Lstat(link); serr == nil && fi.IsDir() {
			if rerr := os.RemoveAll(link); rerr == nil {
				if rerr2 := os.Rename(tmp, link); rerr2 == nil {
					return nil
				}
			}
		}
		return fmt.Errorf("激活 %s 失败: %w", p, err)
	}
	return nil
}

// Deactivate 实现 PublicationStore 接口（幂等）。
func (s *LocalPublicationStore) Deactivate(path string) error {
	p, err := NormalizeURL(path)
	if err != nil {
		return err
	}
	link := filepath.Join(s.ActiveRoot, relActivePath(p))
	if _, err = os.Lstat(link); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Remove(link)
}

// Inspect 实现 PublicationStore 接口。
func (s *LocalPublicationStore) Inspect(path string) (*PublicationState, error) {
	p, err := NormalizeURL(path)
	if err != nil {
		return nil, err
	}
	link := filepath.Join(s.ActiveRoot, relActivePath(p))
	fi, err := os.Lstat(link)
	if err != nil {
		if os.IsNotExist(err) {
			return &PublicationState{Kind: PublicationNone}, nil
		}
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return nil, fmt.Errorf("路径 %s 已被非符号链接占位（状态异常）", p)
	}
	target, err := os.Readlink(link)
	if err != nil {
		return nil, err
	}

	// 解析物理目标：相对 symlink 所在目录。
	resolved := filepath.Clean(filepath.Join(filepath.Dir(link), target))
	if _, err = os.Stat(resolved); err != nil {
		return nil, fmt.Errorf("路径 %s 的激活目标不可达: %w", p, err)
	}

	// 从相对 target 还原 locator key（形如 ../../../artifacts/{hash}，段数随路径深度变化）。
	key := target
	for strings.HasPrefix(key, "../") {
		key = strings.TrimPrefix(key, "../")
	}
	key = strings.ReplaceAll(key, string(filepath.Separator), "/")
	loc := &Locator{Provider: "local", Key: key}

	// redirect 判定：目标目录存在 redirect.json。
	if _, err = os.Stat(filepath.Join(resolved, "redirect.json")); err == nil {
		r, rerr := s.readRedirect(resolved)
		if rerr != nil {
			return nil, rerr
		}
		return &PublicationState{Kind: PublicationRedirect, Locator: loc, Redirect: &r.Directive}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return &PublicationState{Kind: PublicationPage, Locator: loc}, nil
}

// readRedirect 读取目标目录中的 redirect.json。
func (s *LocalPublicationStore) readRedirect(dir string) (*RedirectArtifact, error) {
	data, err := os.ReadFile(filepath.Join(dir, "redirect.json"))
	if err != nil {
		return nil, fmt.Errorf("读取重定向指令失败: %w", err)
	}
	d, err := ParseRedirectEntry(data)
	if err != nil {
		return nil, err
	}
	return &RedirectArtifact{Hash: SHA256(data), Directive: *d, Entry: data}, nil
}

// relActivePath URL path → active 目录相对路径；根路径映射为 index（静态站点入口约定）。
func relActivePath(p string) string {
	rel := strings.TrimPrefix(p, "/")
	if rel == "" {
		return "index"
	}
	return rel
}
