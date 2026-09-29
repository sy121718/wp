package pipeline

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	"go_wp/internal/builder"
)

// manifestDir Manifest.dir 的取值：与 HTML 同一条规则（RTL 落字节、LTR 省略）。
//
// 同源是硬要求：判据分成两份，就会出现「Manifest 说 rtl、产物没有 dir」这类
// 两边都不报错的漂移，而它恰恰是审计要防的那类不一致。
func manifestDir(lang string) string {
	return builder.DirAttr(lang)
}

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
	ManifestSchemaVersion     int    `json:"manifestSchemaVersion"`
	PageDocumentSchemaVersion int    `json:"pageDocumentSchemaVersion"`
	CompilerVersion           string `json:"compilerVersion"`
	SourceID                  string `json:"sourceId"`
	SourceType                string `json:"sourceType"`
	CanonicalPath             string `json:"canonicalPath"`
	SourceHash                string `json:"sourceHash"`
	BuildInputHash            string `json:"buildInputHash"`
	// Lang 本次构建的目标语言（多语言 P2，docs/06-D §4.2 决策 D2：lang 进 Manifest）。
	//
	// 必须显式记录：不同语言产物内容不同，若不进 Manifest 可能两个语言 hash 相同
	// 而内容不同，回滚校验与去重都会失效。omitempty 让未接入语言的来源
	// （如 presentation 自动发布，P5 前不涉及）保持原 Manifest 字节不变。
	//
	// 影响面：产物 hash = SHA256(manifestJSON + "\n" + indexHTML) 含 Manifest
	// （artifact.go artifactPayloadHash），因此新增 lang 字段会改变全部
	// 「带语言构建」的产物 hash，需要一次性全量重建（历史产物仍可按旧 hash 回滚）。
	Lang string `json:"lang,omitempty"`
	// Dir 本次构建的书写方向（审计 I18N-02）：只在 RTL 时落字节。
	//
	// 与 HTML 同一条规则（builder.documentDir）：LTR 是缺省方向，写出来是冗余字节，
	// 而它一进 Manifest 就会改变全部存量产物的 hash。判据同源于
	// builder.LocaleDirection —— Manifest 与 HTML 不可能一个说 rtl 一个没有。
	Dir string `json:"dir,omitempty"`
	// SiteLangs 本次发布冻结的站点语言表（默认语言在前，审计 I18N-02）。
	//
	// 只在发布口径登记（构建输入里确实有一份冻结的语言表时）。它的作用是让
	// 「这次发布依据哪几种语言」成为产物自身的事实：事后审计不再需要去猜
	// 「当时 project_locales 里有什么」——那已经变了。
	SiteLangs []string `json:"siteLangs,omitempty"`
	// SiteDefaultLang 本次发布冻结的站点默认语言（审计 I18N-01）。
	//
	// 与 SiteLangs 成对：语言表回答「有哪几种」，默认语言回答「哪一条是 x-default、
	// 哪一条在 default_plain 方案下不带前缀」。只冻结前者的话，project_locales 里
	// is_default 一改，既有产物重建后 x-default 就换了目标 —— 而语言集合可能一个
	// 都没变，看上去完全不像「配置改过」。
	//
	// omitempty 是确定性与历史兼容的关键：没有冻结语言输入的产物（预览编译、
	// 未接入发布计划的来源）不带这个键，字节与加字段前逐字节一致（hash 不变）。
	SiteDefaultLang string `json:"siteDefaultLang,omitempty"`
	// TranslationMisses 构建期内容译文缺失统计（审计 I18N-02）。
	//
	// 为什么进 Manifest 而不是只记日志：日志是给人看的、会滚动消失、也无法在发布
	// 验收里被机器判定。这里记的是「这次产物里有多少字段用了回退原文」，是发布质量
	// 检查该读的事实。
	TranslationMisses *ManifestTranslationMisses `json:"translationMisses,omitempty"`
	Dependencies      []Dependency               `json:"dependencies"`
	Files             map[string]string          `json:"files"`
	// Diagnostics 本次构建**被容忍**的降级归因（审计 ARCH-05）。
	//
	// 记什么：绑定了、拿得到、但内容为空（ref_empty）这类不构成配置错误的降级，
	// 以及结构模板为空时的块回退。**不记什么**：显式绑定却拿不到 ——
	// 那一类在发布模式直接让构建失败，而失败路径根本没有 Manifest 可写。
	//
	// omitempty 是确定性与历史兼容的关键：没有诊断时该字段完全不出现在 JSON 里，
	// 产物字节与加字段前逐字节一致（hash 不变，无需全量重建）。
	Diagnostics []builder.Degrade `json:"diagnostics,omitempty"`
	// Access 访问面守卫标记（PIPE-6）。nil = 公开页面，字段完全不出现。
	//
	// omitempty 是确定性与历史兼容的关键（同 Diagnostics）：全部存量页面都是公开的，
	// 不带这个键时产物字节与加字段前逐字节一致 —— 否则一次升级会改掉全站产物 hash，
	// 触发全量重建（不变量 5 的可用性一面）。
	//
	// 只放类型，不放 bcrypt 哈希：见 ManifestAccess 的说明。
	Access *ManifestAccess `json:"access,omitempty"`
}

// TranslationPolicyFallback 内容译文缺失时的字段策略：回退原文并计数（当前唯一实现）。
//
// 策略是显式字段而不是隐含行为：验收规则是「缺译的产物能不能上线」由策略决定，
// 现在只有 fallback（缺译照常上线，但统计进 Manifest）；将来若引入 required，
// 消费方按同一个字段判定，不需要新认一个标记。
const TranslationPolicyFallback = "fallback"

// ManifestTranslationMisses 内容译文缺失统计（按字段计数）。
//
// Candidates 是本次编译收集到的可翻译候选字段数，Misses 是其中未命中译文的字段数。
// 两者一起才可判读：misses=3 在 candidates=3（整页没翻译）与 candidates=300
// （只有 3 个字段漏翻）是完全不同的质量问题。
type ManifestTranslationMisses struct {
	Policy     string `json:"policy"`
	Candidates int    `json:"candidates"`
	Misses     int64  `json:"misses"`
}

// 依赖类型常量（docs/03-pipeline.md §8.2 / docs/06-D §10.4）。
const (
	// DependencyKindI18N 界面文案词条依赖：sys_i18n 变更后需重建产物
	// （组件固定文案由构建期取词注入 HTML 字节）。
	DependencyKindI18N = "i18n"
)

// I18NDependencyKey 文案词条依赖的资源键（访客面 namespace）。
const I18NDependencyKey = "i18n:site"

// I18NDependency 构造文案词条依赖条目（revision 取自 sys_i18n_revision，
// 或退化为 max(update_time) 的 RFC3339 串）。
func I18NDependency(revision string) Dependency {
	return Dependency{Kind: DependencyKindI18N, Key: I18NDependencyKey, Revision: revision}
}

// I18NContentDependencyKey 内容译文依赖的资源键（sys_translation，多语言 P5b）。
//
// 与 I18NDependencyKey（i18n:site，sys_i18n 开发者词条）并列，Kind 同为 i18n：
// 两者是同一维度的两种资源，产物字节都受它们影响，重建判定方式一致。
const I18NContentDependencyKey = "i18n:content"

// I18NContentDependency 构造内容译文依赖条目（revision 取自 sys_translation 的
// max(update_time)，见 pkg/i18n.ContentRevision）。
//
// 为什么必须记（docs/06-D §9 关键约束）：缺译文时构建期**回退原文**，补齐译文后
// 若依赖里没有这条记录，revision 未变 → 不触发重建 → 站点长期停留在回退内容。
// 因此「本页用到了内容翻译」（存在可翻译候选字段）就必须登记本条依赖。
func I18NContentDependency(revision string) Dependency {
	return Dependency{Kind: DependencyKindI18N, Key: I18NContentDependencyKey, Revision: revision}
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

// artifactPayloadHash 计算产物内容哈希：SHA256(manifestJSON + "\n" + indexHTML)。
// 存储（NewArtifact）与读取校验（GetArtifact）必须使用同一拼接规则——
// 单次预分配拼接，替代原先双层 append 的晦涩写法。
func artifactPayloadHash(mJSON, html []byte) string {
	payload := make([]byte, 0, len(mJSON)+1+len(html))
	payload = append(payload, mJSON...)
	payload = append(payload, '\n')
	payload = append(payload, html...)
	return SHA256(payload)
}

// EncodeManifest 序列化 manifest（确定性：dependencies 排序 + files 由标准库按 key 排序）。
func EncodeManifest(m *Manifest) ([]byte, error) {
	if m.Dependencies != nil {
		slices.SortStableFunc(m.Dependencies, func(a, b Dependency) int {
			return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Key, b.Key))
		})
	}
	return json.Marshal(m)
}

// NewArtifact 组装不可变产物：入口 HTML + manifest → 内容寻址哈希。
// docs/03-pipeline.md §4.1：Artifact 目录保存入口与 manifest，文件名不含可变别名。
func NewArtifact(html []byte, m *Manifest) (*Artifact, error) {
	return NewArtifactWithEntries(html, m, nil)
}

// NewArtifactWithEntries 同 NewArtifact，但额外落盘一组伴随文件（PIPE-6 的守卫页
// guard.html / guard.json）。
//
// 为什么单开一个函数而不是给 NewArtifact 加参数：NewArtifact 还有第二个调用方
// （presentation 自动发布，presentation_render.go），自动发布不消费页面级访问设置，
// 改签名只会让它多传一个 nil —— 参数化一个只有一条路径用得上的能力，
// 换来的是每个调用点都要解释「这里为什么是 nil」。
//
// extra 的每个文件都会登记进 Manifest.Files（内容哈希 → 参与产物 hash）：
// 「同一产物目录里存在两个不同内容」这件事，只能靠 hash 覆盖到所有文件来排除。
// 哈希不覆盖 guard.json 的后果是具体的 —— 改密码后 hash 不变，
// PutArtifact 走幂等分支，新密码永远写不进去。
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
		"manifest.json": nil, // 占位：编码完成后再填
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
	hash := artifactPayloadHash(mJSON, html)

	return &Artifact{
		Hash:          hash,
		CanonicalPath: m.CanonicalPath,
		Manifest:      *m,
		Entries:       entries,
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
