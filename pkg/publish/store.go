package publish

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"os"
	"path/filepath"
	"strings"
)

type Locator struct {
	Provider string `json:"provider"`

	Key string `json:"key"`
}

func ArtifactLocator(hash string) Locator {
	return Locator{Provider: "local", Key: "artifacts/" + hash}
}

type Store interface {
	PutArtifact(a *Artifact) (Locator, error)

	GetArtifact(loc Locator) (*Artifact, error)

	VerifyArtifact(loc Locator, expectedHash string) error

	DeleteArtifact(loc Locator, expectedHash string) error

	DeleteArtifactObject(loc Locator, relPath string) error

	PutRedirect(r *RedirectArtifact) (Locator, error)

	GetRedirect(loc Locator) (*RedirectArtifact, error)

	DeleteRedirect(loc Locator, expectedHash string) error
}

type LocalStore struct {
	Root string
}

func (s *LocalStore) artifactDir(hash string) string {
	return filepath.Join(s.Root, "artifacts", hash)
}

func (s *LocalStore) redirectDir(hash string) string {
	return filepath.Join(s.Root, "artifacts", "redirects", hash)
}

func (s *LocalStore) PutArtifact(a *Artifact) (Locator, error) {
	if a == nil || a.Hash == "" {
		return Locator{}, fmt.Errorf("产物为空或缺少内容哈希")
	}
	dir := s.artifactDir(a.Hash)
	loc := ArtifactLocator(a.Hash)

	if _, err := os.Stat(dir); err == nil {

		if verr := s.VerifyArtifact(loc, a.Hash); verr != nil {
			return Locator{}, fmt.Errorf("产物目录已存在但内容不一致（禁止覆盖）: %w", verr)
		}
		return loc, nil
	} else if !os.IsNotExist(err) {
		return Locator{}, err
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Locator{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+".tmp-*")
	if err != nil {
		return Locator{}, err
	}
	defer os.RemoveAll(tmp)

	for path, data := range a.Entries {
		if !filepath.IsLocal(path) {
			return Locator{}, fmt.Errorf("产物包含非法相对路径: %q", path)
		}
		if err := writeFileSync(filepath.Join(tmp, path), data); err != nil {
			return Locator{}, err
		}
	}

	for _, required := range []string{"manifest.json", "index.html"} {
		if _, ok := a.Entries[required]; !ok {
			return Locator{}, fmt.Errorf("产物缺少必需文件: %q", required)
		}
	}

	if f, oerr := os.Open(tmp); oerr == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	if err = os.Rename(tmp, dir); err != nil {

		if _, serr := os.Stat(dir); serr == nil {
			if verr := s.VerifyArtifact(loc, a.Hash); verr != nil {
				return Locator{}, fmt.Errorf("产物目录已存在但内容不一致（禁止覆盖）: %w", verr)
			}
			return loc, nil
		}
		return Locator{}, err
	}
	return loc, nil
}

func (s *LocalStore) GetArtifact(loc Locator) (*Artifact, error) {
	hash := locatorHash(loc)
	if hash == "" {
		return nil, fmt.Errorf("定位器缺少内容哈希: %q", loc.Key)
	}
	dir := s.artifactDir(hash)

	mJSON, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("读取 manifest 失败: %w", err)
	}
	html, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("读取 index.html 失败: %w", err)
	}
	var m Manifest
	if err = json.Unmarshal(mJSON, &m); err != nil {
		return nil, fmt.Errorf("manifest 解析失败: %w", err)
	}
	if got := artifactPayloadHash(mJSON, html); got != hash {
		return nil, fmt.Errorf("产物内容哈希校验失败（期望 %s 实际 %s）", hash, got)
	}
	return &Artifact{
		Hash:          hash,
		CanonicalPath: m.CanonicalPath,
		Manifest:      m,
		Entries: map[string][]byte{
			"index.html":    html,
			"manifest.json": mJSON,
		},
	}, nil
}

func (s *LocalStore) VerifyArtifact(loc Locator, expectedHash string) error {
	if _, err := os.Stat(s.artifactDir(locatorHash(loc))); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("产物不存在: %q", loc.Key)
		}
		return err
	}
	a, err := s.GetArtifact(loc)
	if err != nil {
		return err
	}
	if a.Hash != expectedHash {
		return fmt.Errorf("产物哈希不一致（期望 %s 实际 %s）", expectedHash, a.Hash)
	}
	return nil
}

func (s *LocalStore) DeleteArtifact(loc Locator, expectedHash string) error {
	hash := locatorHash(loc)
	dir := s.artifactDir(hash)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := s.VerifyArtifact(loc, expectedHash); err != nil {
		return fmt.Errorf("删除前校验失败: %w", err)
	}
	return os.RemoveAll(dir)
}

func (s *LocalStore) DeleteArtifactObject(loc Locator, relPath string) error {
	if relPath == "" || strings.Contains(relPath, "..") || strings.HasPrefix(relPath, "/") {
		return fmt.Errorf("非法文件路径: %q", relPath)
	}
	p := filepath.Join(s.artifactDir(locatorHash(loc)), relPath)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Remove(p)
}

func (s *LocalStore) PutRedirect(r *RedirectArtifact) (Locator, error) {
	if r == nil || r.Hash == "" {
		return Locator{}, fmt.Errorf("重定向产物为空或缺少内容哈希")
	}
	dir := s.redirectDir(r.Hash)
	loc := Locator{Provider: "local", Key: "artifacts/redirects/" + r.Hash}

	if _, err := os.Stat(dir); err == nil {

		got, gerr := s.GetRedirect(loc)
		if gerr != nil || got.Hash != r.Hash {
			return Locator{}, fmt.Errorf("重定向产物目录已存在但内容不一致（禁止覆盖）")
		}
		return loc, nil
	} else if !os.IsNotExist(err) {
		return Locator{}, err
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Locator{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+".tmp-*")
	if err != nil {
		return Locator{}, err
	}
	defer os.RemoveAll(tmp)
	if err = os.WriteFile(filepath.Join(tmp, "redirect.json"), r.Entry, 0o644); err != nil {
		return Locator{}, err
	}
	if err = os.Rename(tmp, dir); err != nil {
		if _, serr := os.Stat(dir); serr == nil {
			got, gerr := s.GetRedirect(loc)
			if gerr != nil || got.Hash != r.Hash {
				return Locator{}, fmt.Errorf("重定向产物目录已存在但内容不一致（禁止覆盖）")
			}
			return loc, nil
		}
		return Locator{}, err
	}
	return loc, nil
}

func (s *LocalStore) GetRedirect(loc Locator) (*RedirectArtifact, error) {
	hash := locatorHash(loc)
	entry, err := os.ReadFile(filepath.Join(s.redirectDir(hash), "redirect.json"))
	if err != nil {
		return nil, fmt.Errorf("读取 redirect.json 失败: %w", err)
	}
	if got := SHA256(entry); got != hash {
		return nil, fmt.Errorf("重定向内容哈希校验失败（期望 %s 实际 %s）", hash, got)
	}
	d, err := ParseRedirectEntry(entry)
	if err != nil {
		return nil, err
	}
	return &RedirectArtifact{Hash: hash, Directive: *d, Entry: entry}, nil
}

func (s *LocalStore) DeleteRedirect(loc Locator, expectedHash string) error {
	hash := locatorHash(loc)
	dir := s.redirectDir(hash)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	got, err := s.GetRedirect(loc)
	if err != nil {
		return fmt.Errorf("删除前校验失败: %w", err)
	}
	if got.Hash != expectedHash {
		return fmt.Errorf("重定向哈希不一致（期望 %s 实际 %s）", expectedHash, got.Hash)
	}
	return os.RemoveAll(dir)
}

func locatorHash(loc Locator) string {
	key := strings.TrimSuffix(loc.Key, "/")
	if idx := strings.LastIndex(key, "/"); idx >= 0 {
		return key[idx+1:]
	}
	return key
}

func writeFileSync(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// LocatorHash 取产物的定位键（供应用侧与测试使用）。
func LocatorHash(loc Locator) string { return locatorHash(loc) }
