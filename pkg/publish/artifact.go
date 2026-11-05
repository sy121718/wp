package publish

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	json "go_wp/pkg/jsonx"
	"path/filepath"
	"slices"

)

func manifestDir(lang string) string {
	return dirAttr(lang)
}

const (
	ManifestSchemaVersion = 1

	SourceTypePage = "page"

	SourceTypePresentation = "presentation"
)

type Dependency struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Revision string `json:"revision"`
}

type Manifest struct {
	ManifestSchemaVersion     int    `json:"manifestSchemaVersion"`
	PageDocumentSchemaVersion int    `json:"pageDocumentSchemaVersion"`
	CompilerVersion           string `json:"compilerVersion"`
	SourceID                  string `json:"sourceId"`
	SourceType                string `json:"sourceType"`
	CanonicalPath             string `json:"canonicalPath"`
	SourceHash                string `json:"sourceHash"`
	BuildInputHash            string `json:"buildInputHash"`

	Lang string `json:"lang,omitempty"`

	Dir string `json:"dir,omitempty"`

	SiteLangs []string `json:"siteLangs,omitempty"`

	SiteDefaultLang string `json:"siteDefaultLang,omitempty"`

	References *ReferenceSnapshot `json:"references,omitempty"`

	TranslationMisses *ManifestTranslationMisses `json:"translationMisses,omitempty"`
	Dependencies      []Dependency               `json:"dependencies"`
	Files             map[string]string          `json:"files"`

	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`

	Access *ManifestAccess `json:"access,omitempty"`
}

const TranslationPolicyFallback = "fallback"

type ManifestTranslationMisses struct {
	Policy     string `json:"policy"`
	Candidates int    `json:"candidates"`
	Misses     int64  `json:"misses"`
}

const (
	DependencyKindI18N = "i18n"
)

const I18NDependencyKey = "i18n:site"

func I18NDependency(revision string) Dependency {
	return Dependency{Kind: DependencyKindI18N, Key: I18NDependencyKey, Revision: revision}
}

const I18NContentDependencyKey = "i18n:content"

func I18NContentDependency(revision string) Dependency {
	return Dependency{Kind: DependencyKindI18N, Key: I18NContentDependencyKey, Revision: revision}
}

type Artifact struct {
	Hash string

	CanonicalPath string

	Manifest Manifest

	Entries map[string][]byte
}

type RedirectDirective struct {
	TargetPath string `json:"targetPath"`

	StatusCode int `json:"statusCode"`
}

type RedirectArtifact struct {
	Hash      string
	Directive RedirectDirective

	Entry []byte
}

func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func artifactPayloadHash(mJSON, html []byte) string {
	payload := make([]byte, 0, len(mJSON)+1+len(html))
	payload = append(payload, mJSON...)
	payload = append(payload, '\n')
	payload = append(payload, html...)
	return SHA256(payload)
}

func EncodeManifest(m *Manifest) ([]byte, error) {
	if m.Dependencies != nil {
		slices.SortStableFunc(m.Dependencies, func(a, b Dependency) int {
			return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Key, b.Key))
		})
	}
	return json.Marshal(m)
}

func NewArtifact(html []byte, m *Manifest) (*Artifact, error) {
	return NewArtifactWithEntries(html, m, nil)
}

func NewArtifactWithEntries(html []byte, m *Manifest, extra map[string][]byte) (*Artifact, error) {
	if m == nil || m.CanonicalPath == "" || m.SourceID == "" {
		return nil, fmt.Errorf("manifest 不完整：必须包含 canonicalPath 与 sourceId")
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	htmlHash := SHA256(html)
	m.Files["index.html"] = htmlHash

	entries := map[string][]byte{
		"index.html":    html,
		"manifest.json": nil,
	}
	for name, data := range extra {
		if name == "" || !filepath.IsLocal(name) {
			return nil, fmt.Errorf("伴随文件路径非法: %q", name)
		}
		if _, exists := entries[name]; exists {
			return nil, fmt.Errorf("伴随文件与产物入口重名: %q", name)
		}
		entries[name] = data
		m.Files[name] = SHA256(data)
	}

	mJSON, err := EncodeManifest(m)
	if err != nil {
		return nil, fmt.Errorf("manifest 编码失败: %w", err)
	}
	entries["manifest.json"] = mJSON

	if err = PrecompressEntries(entries); err != nil {
		return nil, err
	}

	hash := artifactPayloadHash(mJSON, html)

	return &Artifact{
		Hash:          hash,
		CanonicalPath: m.CanonicalPath,
		Manifest:      *m,
		Entries:       entries,
	}, nil
}

func NewRedirectArtifact(targetPath string, statusCode int) (*RedirectArtifact, error) {
	p, err := NormalizeURL(targetPath)
	if err != nil {
		return nil, err
	}
	if statusCode != 301 && statusCode != 302 {
		return nil, fmt.Errorf("重定向状态码仅支持 301/302: %d", statusCode)
	}
	entry, err := json.Marshal(RedirectDirective{TargetPath: p, StatusCode: statusCode})
	if err != nil {
		return nil, fmt.Errorf("重定向指令编码失败: %w", err)
	}
	return &RedirectArtifact{
		Hash:      SHA256(entry),
		Directive: RedirectDirective{TargetPath: p, StatusCode: statusCode},
		Entry:     entry,
	}, nil
}

func ParseRedirectEntry(data []byte) (*RedirectDirective, error) {
	var d RedirectDirective
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("重定向指令解析失败: %w", err)
	}
	if d.TargetPath == "" {
		return nil, fmt.Errorf("重定向指令缺少目标路径")
	}
	return &d, nil
}

// ManifestDirForLang 返回发布产物的语言目录。
func ManifestDirForLang(lang string) string { return manifestDir(lang) }
