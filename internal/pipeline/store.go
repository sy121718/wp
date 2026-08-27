package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Locator 产物定位器（docs/03-pipeline.md §4.3）：不保存 endpoint、凭据或临时签名。
type Locator struct {
	Provider string `json:"provider"`
	// Key 存储键；本地实现为 artifacts/{hash} 或 artifacts/redirects/{hash}。
	Key string `json:"key"`
}

// Store ArtifactStore 契约（docs/03-pipeline.md §4.3）。
// 删除接口只接受持久的 Locator 与期望 hash，必须幂等；Store 自身不判断业务引用，
// 引用检查由生命周期服务（Phase 0-A1 publication 模块）负责。
type Store interface {
	// PutArtifact 写入不可变 Page Artifact；已存在同 hash 直接返回（幂等，禁止覆盖）。
	PutArtifact(a *Artifact) (Locator, error)
	// GetArtifact 按 locator 读取 Artifact 并重新校验内容哈希。
	GetArtifact(loc Locator) (*Artifact, error)
	// VerifyArtifact 校验 locator 指向产物的内容哈希与期望一致。
	VerifyArtifact(loc Locator, expectedHash string) error
	// DeleteArtifact 幂等删除：仅当现有内容哈希与 expectedHash 一致时执行。
	DeleteArtifact(loc Locator, expectedHash string) error
	// DeleteArtifactObject 幂等删除 Artifact 内的单个文件（内容对象）。
	DeleteArtifactObject(loc Locator, relPath string) error

	// PutRedirect 写入重定向产物（docs/03-pipeline.md §4.4）。
	PutRedirect(r *RedirectArtifact) (Locator, error)
	// GetRedirect 按 locator 读取重定向产物并重新校验内容哈希。
	GetRedirect(loc Locator) (*RedirectArtifact, error)
	// DeleteRedirect 幂等删除重定向产物。
	DeleteRedirect(loc Locator, expectedHash string) error
}

// LocalStore 本地文件系统 ArtifactStore：内容寻址目录，永不覆盖历史文件。
//
// 布局（docs/03-pipeline.md §4.1/§4.4）：
//
//	{root}/artifacts/{hash}/index.html
//	{root}/artifacts/{hash}/manifest.json
//	{root}/artifacts/redirects/{hash}/redirect.json
type LocalStore struct {
	// Root 存储根目录（生产为配置的 artifacts 根，测试用临时目录）。
	Root string
}

// artifactDir 计算 Artifact 目录。
func (s *LocalStore) artifactDir(hash string) string {
	return filepath.Join(s.Root, "artifacts", hash)
}

// redirectDir 计算重定向产物目录。
func (s *LocalStore) redirectDir(hash string) string {
	return filepath.Join(s.Root, "artifacts", "redirects", hash)
}

// PutArtifact 实现 Store 接口。
func (s *LocalStore) PutArtifact(a *Artifact) (Locator, error) {
	if a == nil || a.Hash == "" {
		return Locator{}, fmt.Errorf("产物为空或缺少内容哈希")
	}
	dir := s.artifactDir(a.Hash)
	loc := Locator{Provider: "local", Key: "artifacts/" + a.Hash}

	if _, err := os.Stat(dir); err == nil {
		// 已存在：验证一致后幂等返回（禁止覆盖历史文件）。
		if verr := s.VerifyArtifact(loc, a.Hash); verr != nil {
			return Locator{}, fmt.Errorf("产物目录已存在但内容不一致（禁止覆盖）: %w", verr)
		}
		return loc, nil
	} else if !os.IsNotExist(err) {
		return Locator{}, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Locator{}, err
	}
	for _, path := range []string{"manifest.json", "index.html"} {
		if err := os.WriteFile(filepath.Join(dir, path), a.Entries[path], 0o644); err != nil {
			return Locator{}, err
		}
	}
	return loc, nil
}

// GetArtifact 实现 Store 接口：读取并重算哈希校验。
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
	if got := SHA256(append(append([]byte{}, mJSON...), append([]byte("\n"), html...)...)); got != hash {
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

// VerifyArtifact 实现 Store 接口。
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

// DeleteArtifact 实现 Store 接口（幂等；hash 不符拒绝删除）。
func (s *LocalStore) DeleteArtifact(loc Locator, expectedHash string) error {
	hash := locatorHash(loc)
	dir := s.artifactDir(hash)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil // 幂等：已删除
		}
		return err
	}
	if err := s.VerifyArtifact(loc, expectedHash); err != nil {
		return fmt.Errorf("删除前校验失败: %w", err)
	}
	return os.RemoveAll(dir)
}

// DeleteArtifactObject 实现 Store 接口（幂等；单文件删除）。
func (s *LocalStore) DeleteArtifactObject(loc Locator, relPath string) error {
	if relPath == "" || strings.Contains(relPath, "..") || strings.HasPrefix(relPath, "/") {
		return fmt.Errorf("非法文件路径: %q", relPath)
	}
	p := filepath.Join(s.artifactDir(locatorHash(loc)), relPath)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return nil // 幂等：已删除
		}
		return err
	}
	return os.Remove(p)
}

// PutRedirect 实现 Store 接口。
func (s *LocalStore) PutRedirect(r *RedirectArtifact) (Locator, error) {
	if r == nil || r.Hash == "" {
		return Locator{}, fmt.Errorf("重定向产物为空或缺少内容哈希")
	}
	dir := s.redirectDir(r.Hash)
	loc := Locator{Provider: "local", Key: "artifacts/redirects/" + r.Hash}

	if _, err := os.Stat(dir); err == nil {
		// 幂等：验证一致直接返回。
		got, gerr := s.GetRedirect(loc)
		if gerr != nil || got.Hash != r.Hash {
			return Locator{}, fmt.Errorf("重定向产物目录已存在但内容不一致（禁止覆盖）")
		}
		return loc, nil
	} else if !os.IsNotExist(err) {
		return Locator{}, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Locator{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "redirect.json"), r.Entry, 0o644); err != nil {
		return Locator{}, err
	}
	return loc, nil
}

// GetRedirect 实现 Store 接口。
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

// DeleteRedirect 实现 Store 接口（幂等；hash 不符拒绝删除）。
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

// locatorHash 从 Locator.Key 提取末尾内容哈希。
func locatorHash(loc Locator) string {
	key := strings.TrimSuffix(loc.Key, "/")
	if idx := strings.LastIndex(key, "/"); idx >= 0 {
		return key[idx+1:]
	}
	return key
}
