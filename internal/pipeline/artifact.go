package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// 常量：manifest 版本号与支持来源类型（docs/03-pipeline.md §4.2）。
const (
	// ManifestSchemaVersion manifest schema 版本。
	ManifestSchemaVersion = 1
	// SourceTypePage 手工 Page 来源。
	SourceTypePage = "page"
	// SourceTypePresentation 自动 PresentationInstance 来源。
	SourceTypePresentation = "presentation"
)

// Dependency 构建期依赖条目（manifest.dependencies，按 (kind,key) 排序）。
type Dependency struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Revision string `json:"revision"`
}

// Manifest Artifact 清单（docs/03-pipeline.md §4.2）。
// 确定性守则（§3.4）：manifest 不写构建时间；files 由标准库按 key 排序；
// dependencies 在编码前按 (kind,key) 显式排序。
type Manifest struct {
	ManifestSchemaVersion     int               `json:"manifestSchemaVersion"`
	PageDocumentSchemaVersion int               `json:"pageDocumentSchemaVersion"`
	CompilerVersion           string            `json:"compilerVersion"`
	SourceID                  string            `json:"sourceId"`
	SourceType                string            `json:"sourceType"`
	CanonicalPath             string            `json:"canonicalPath"`
	SourceHash                string            `json:"sourceHash"`
	BuildInputHash            string            `json:"buildInputHash"`
	Dependencies              []Dependency      `json:"dependencies"`
	Files                     map[string]string `json:"files"`
}

// Artifact 不可变发布产物：入口 HTML + manifest + 其他 manifest 声明文件。
// 一旦 Put 到 ArtifactStore 后禁止修改；重新构建产生新 hash 新目录。
type Artifact struct {
	// Hash 内容寻址哈希 = sha256(manifestJSON + "\n" + indexHTML)。
	Hash string
	// CanonicalPath 产物绑定的规范化访问路径（docs/03-pipeline.md §6.4 回滚校验）。
	CanonicalPath string
	// Manifest 产物清单。
	Manifest Manifest
	// Entries 物理文件内容：path（相对 artifacts 目录）→ 字节。
	Entries map[string][]byte
}

// RedirectDirective 重定向指令（docs/03-pipeline.md §4.4）：极简 Artifact 内容。
type RedirectDirective struct {
	TargetPath string `json:"targetPath"`
	// StatusCode 301 永久（URL 修改默认）/ 302 临时。
	StatusCode int `json:"statusCode"`
}

// RedirectArtifact 重定向产物（不经过 Publish Compiler，只有 redirect.json）。
type RedirectArtifact struct {
	Hash      string
	Directive RedirectDirective
	// Entry 序列化后的 redirect.json 字节。
	Entry []byte
}

// SHA256 计算内容哈希（十六进制小写）。
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// EncodeManifest 序列化 manifest（确定性：dependencies 排序 + files 由标准库按 key 排序）。
func EncodeManifest(m *Manifest) ([]byte, error) {
	if m.Dependencies != nil {
		sort.SliceStable(m.Dependencies, func(i, j int) bool {
			if m.Dependencies[i].Kind != m.Dependencies[j].Kind {
				return m.Dependencies[i].Kind < m.Dependencies[j].Kind
			}
			return m.Dependencies[i].Key < m.Dependencies[j].Key
		})
	}
	return json.Marshal(m)
}

// NewArtifact 组装不可变产物：入口 HTML + manifest → 内容寻址哈希。
// docs/03-pipeline.md §4.1：Artifact 目录保存入口与 manifest，文件名不含可变别名。
func NewArtifact(html []byte, m *Manifest) (*Artifact, error) {
	if m == nil || m.CanonicalPath == "" || m.SourceID == "" {
		return nil, fmt.Errorf("manifest 不完整：必须包含 canonicalPath 与 sourceId")
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	htmlHash := SHA256(html)
	m.Files["index.html"] = htmlHash

	mJSON, err := EncodeManifest(m)
	if err != nil {
		return nil, fmt.Errorf("manifest 编码失败: %w", err)
	}
	hash := SHA256(append(append([]byte{}, mJSON...), append([]byte("\n"), html...)...))

	return &Artifact{
		Hash:          hash,
		CanonicalPath: m.CanonicalPath,
		Manifest:      *m,
		Entries: map[string][]byte{
			"index.html":    html,
			"manifest.json": mJSON,
		},
	}, nil
}

// NewRedirectArtifact 构建重定向产物（docs/03-pipeline.md §4.4）。
// 目标路径必须已规范化；状态码仅允许 301/302。
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

// ParseRedirectEntry 解析 redirect.json 内容（Static Server 检测用）。
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
