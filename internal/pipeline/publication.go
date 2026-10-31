package pipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

var activatingSeq atomic.Uint64

const (
	PublicationPage = "page"

	PublicationRedirect = "redirect"

	PublicationNone = "none"
)

type PublicationState struct {
	Kind string

	Locator *Locator

	Redirect *RedirectDirective

	Guard *GuardMeta
}

type PublicationStore interface {
	Activate(path string, loc Locator) error

	Deactivate(path string) error

	Inspect(path string) (*PublicationState, error)
}

type LocalPublicationStore struct {
	ActiveRoot string
}

func (s *LocalPublicationStore) Activate(path string, loc Locator) error {
	p, err := NormalizeURL(path)
	if err != nil {
		return err
	}
	if loc.Provider == "" || loc.Key == "" {
		return fmt.Errorf("定位器缺少 provider 或 key")
	}

	rel := relActivePath(p)

	if err = s.ensureAncestorsAreDirs(rel); err != nil {
		return fmt.Errorf("激活 %s 失败: %w", p, err)
	}
	link := filepath.Join(s.ActiveRoot, rel)
	parent := filepath.Dir(link)
	if err = os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	up := strings.Split(relActivePath(p), "/")
	target := strings.Repeat("../", len(up)+1) + loc.Key

	tmp := fmt.Sprintf("%s.activating-%d-%d-%s", link, os.Getpid(), activatingSeq.Add(1), locatorHash(loc))

	if err = os.Symlink(target, tmp); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}

		if fi, serr := os.Lstat(tmp); serr == nil {
			if fi.IsDir() {
				return fmt.Errorf("激活 %s 失败: 临时名 %s 已被目录占用", p, tmp)
			}
			if rerr := os.Remove(tmp); rerr != nil && !os.IsNotExist(rerr) {
				return fmt.Errorf("激活 %s 失败: 清理残留临时链接 %s: %w", p, tmp, rerr)
			}
		}
		if err = os.Symlink(target, tmp); err != nil {
			return err
		}
	}

	if err = os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("激活 %s 失败: %w", p, err)
	}
	return nil
}

func (s *LocalPublicationStore) ensureAncestorsAreDirs(rel string) error {
	segs := strings.Split(rel, "/")
	cur := s.ActiveRoot
	for i := range len(segs) - 1 {
		cur = filepath.Join(cur, segs[i])
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("父路径 %s 已被符号链接占位（父子路径互斥，禁止写入不可变产物目录）", cur)
		}
		if !fi.IsDir() {
			return fmt.Errorf("父路径 %s 已被非目录占位", cur)
		}
	}
	return nil
}

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

	resolved := filepath.Clean(filepath.Join(filepath.Dir(link), target))
	if _, err = os.Stat(resolved); err != nil {
		return nil, fmt.Errorf("路径 %s 的激活目标不可达: %w", p, err)
	}

	key := target
	for strings.HasPrefix(key, "../") {
		key = strings.TrimPrefix(key, "../")
	}
	key = strings.ReplaceAll(key, string(filepath.Separator), "/")
	loc := &Locator{Provider: "local", Key: key}

	if _, err = os.Stat(filepath.Join(resolved, "redirect.json")); err == nil {
		r, rerr := s.readRedirect(resolved)
		if rerr != nil {
			return nil, rerr
		}
		return &PublicationState{Kind: PublicationRedirect, Locator: loc, Redirect: &r.Directive}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	g, gerr := ReadGuardMeta(resolved)
	if gerr != nil {
		return nil, gerr
	}
	return &PublicationState{Kind: PublicationPage, Locator: loc, Guard: g}, nil
}

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

type ActiveLinkIssue struct {
	URLPath string `json:"urlPath"`

	Link string `json:"link"`

	Reason string `json:"reason"`
}

func (s *LocalPublicationStore) AuditActiveLinks() (issues []ActiveLinkIssue, checked int, err error) {
	if s.ActiveRoot == "" {
		return nil, 0, fmt.Errorf("激活目录未配置")
	}
	if _, serr := os.Stat(s.ActiveRoot); serr != nil {
		if os.IsNotExist(serr) {
			return nil, 0, nil
		}
		return nil, 0, serr
	}
	walkErr := filepath.WalkDir(s.ActiveRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			issues = append(issues, ActiveLinkIssue{Link: path, Reason: "遍历失败: " + werr.Error()})
			return nil
		}
		if d.IsDir() {
			return nil
		}

		if rel, rerr := filepath.Rel(s.ActiveRoot, path); rerr == nil && IsSiteRootFile(rel) {
			return nil
		}

		if d.Type()&os.ModeSymlink == 0 {
			issues = append(issues, ActiveLinkIssue{
				URLPath: activeRelToURL(s.ActiveRoot, path),
				Link:    path,
				Reason:  "非符号链接（激活目录只应存放指向产物的链接）",
			})
			return nil
		}
		checked++
		if _, serr := os.Stat(path); serr != nil {
			issues = append(issues, ActiveLinkIssue{
				URLPath: activeRelToURL(s.ActiveRoot, path),
				Link:    path,
				Reason:  "链接目标不可达: " + serr.Error(),
			})
		}
		return nil
	})
	if walkErr != nil {
		return issues, checked, walkErr
	}
	return issues, checked, nil
}

func activeRelToURL(root, path string) string {
	rel, rerr := filepath.Rel(root, path)
	if rerr != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "index" || rel == "index.html" {
		return "/"
	}
	return "/" + rel
}

func relActivePath(p string) string {
	rel := strings.TrimPrefix(p, "/")
	if rel == "" {
		return "index"
	}
	return rel
}
