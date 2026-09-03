// Package media 实现 go_wp 媒体中心的领域内核（规范 docs/02-B）：
//
//   - 不可变资产与稳定引用：文件上传生成唯一 assetId（内容哈希派生）与内容哈希；
//   - 物理存储与逻辑分类解耦：分类/标签是纯元数据，调整分类不改变物理路径与 URL；
//   - 引用追踪与保护：记录资产的引用方，被引用资产删除强制拦截并列出引用清单。
//
// 自「WordPress 真实模式」起，内容引用面（组件）只存 URL 快照，构建期零解析；
// 本包保留资产/变体/引用追踪领域模型，供未来 media_asset 三表落地对接。
package media

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// 文件类型常量。
const (
	TypeImage    = "image"
	TypeVideo    = "video"
	TypeSVG      = "svg"
	TypeDocument = "document"
)

// 变体规格常量。
const (
	VariantThumbnail = "thumbnail"
	VariantMedium    = "medium"
	VariantLarge     = "large"
	VariantOriginal  = "original"
)

// assetIDPrefix 本包派生 assetId 的前缀。
const assetIDPrefix = "ast_"

var (
	// hashRe 内容哈希白名单：十六进制 32~128 位。
	hashRe = regexp.MustCompile(`^[a-fA-F0-9]{32,128}$`)
	// tagRe 标签白名单：中文/字母/数字，1~50 字符。
	tagRe = regexp.MustCompile(`^[\p{Han}A-Za-z0-9_-]{1,50}$`)
)

// Variant 资产变体：尺寸裁剪或现代格式转码产物（上传后自动生成）。
type Variant struct {
	// Kind 规格标识：thumbnail / medium / large / original。
	Kind string `json:"kind"`
	// Format 文件格式：jpeg / png / webp / avif。
	Format string `json:"format"`
	// URL 变体稳定访问地址。
	URL string `json:"url"`
	// Width / Height 变体宽高（像素）。原始宽高在构建期写入 <img>，杜绝 CLS。
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Asset 媒体资产：不可变资产与稳定引用的载体。
type Asset struct {
	// ID 稳定标识（内容哈希派生）。版本替换时保持不变。
	ID string `json:"id"`
	// Hash 内容哈希（去重与替换检测依据）。
	Hash string `json:"hash"`
	// FileName 原始文件名。
	FileName string `json:"fileName"`
	// MimeType MIME 类型。
	MimeType string `json:"mimeType"`
	// Type 文件类型：image / video / svg / document。
	Type string `json:"type"`
	// Width / Height 原始宽高（图片类必填）。
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// Size 文件字节数。
	Size int64 `json:"size"`
	// 全局 SEO 元数据：组件引用时默认继承，Inspector 可局部覆盖。
	Alt     string `json:"alt,omitempty"`
	Title   string `json:"title,omitempty"`
	Caption string `json:"caption,omitempty"`
	// Variants 变体集合。
	Variants []Variant `json:"variants,omitempty"`
	// CategoryID 分类（纯元数据，与物理存储无关）。
	CategoryID string `json:"categoryId,omitempty"`
	// Tags 标签。
	Tags []string `json:"tags,omitempty"`
	// Generation 替换代数：版本替换 +1，用于触发全站静态产物刷新。
	Generation int `json:"generation"`
}

// Reference 资产引用记录：被哪个业务实体引用。
type Reference struct {
	// Kind 引用方类型：page / product / article / global_component / site_setting。
	Kind string `json:"kind"`
	// ID 引用方标识。
	ID string `json:"id"`
	// Title 引用方标题（供拦截警告展示）。
	Title string `json:"title"`
}

// Store 媒体资产内存存储：去重索引、引用登记与删除保护。
type Store struct {
	mu     sync.RWMutex
	assets map[string]*Asset
	byHash map[string][]string             // hash → asset IDs（去重检测）
	refs   map[string]map[string]Reference // assetID → 引用方（kind:id 去重）
}

// NewStore 创建空存储。
func NewStore() *Store {
	return &Store{
		assets: map[string]*Asset{},
		byHash: map[string][]string{},
		refs:   map[string]map[string]Reference{},
	}
}

// Upload 上传资产：基于内容哈希派生稳定 assetId；
// duplicateOf 非空表示同哈希资产已存在（重复上传，调用方可提示"替换原文件"）。
func (s *Store) Upload(a Asset) (assetID, duplicateOf string, err error) {
	if !hashRe.MatchString(a.Hash) {
		return "", "", fmt.Errorf("无效的内容哈希: %q", a.Hash)
	}
	switch a.Type {
	case TypeImage, TypeSVG:
		if a.Width <= 0 || a.Height <= 0 {
			return "", "", errors.New("图片类资产必须提供原始宽高")
		}
	case TypeVideo, TypeDocument:
	default:
		return "", "", fmt.Errorf("无效的文件类型: %q", a.Type)
	}
	for _, t := range a.Tags {
		if !tagRe.MatchString(t) {
			return "", "", fmt.Errorf("无效的标签: %q", t)
		}
	}
	a.ID = deriveAssetID(a.Hash)
	a.Generation = 1

	s.mu.Lock()
	defer s.mu.Unlock()
	if ids, ok := s.byHash[a.Hash]; ok && len(ids) > 0 {
		return a.ID, ids[0], nil
	}
	s.assets[a.ID] = &a
	s.byHash[a.Hash] = append(s.byHash[a.Hash], a.ID)
	return a.ID, "", nil
}

// deriveAssetID 由内容哈希派生稳定标识（规范：唯一稳定标识，禁止动态临时路径）。
func deriveAssetID(hash string) string {
	return assetIDPrefix + strings.ToLower(hash[:24])
}

// Get 查询资产；不存在时返回错误。
func (s *Store) Get(assetID string) (a *Asset, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.assets[assetID]
	if !ok {
		return nil, fmt.Errorf("媒体资产不存在: %s", assetID)
	}
	cp := *asset
	return &cp, nil
}

// FindByHash 按内容哈希查找已存在资产（上传去重检测）。
func (s *Store) FindByHash(hash string) (a *Asset, found bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.byHash[hash]
	if len(ids) == 0 {
		return nil, false
	}
	asset := s.assets[ids[0]]
	cp := *asset
	return &cp, true
}

// Replace 版本替换：保留原 assetId 与引用关系，仅更新内容与变体；
// Generation+1 用于触发全站该图片静态编译产物的一键刷新。
func (s *Store) Replace(assetID, newHash string, width, height int, size int64, variants []Variant) (err error) {
	if !hashRe.MatchString(newHash) {
		return fmt.Errorf("无效的内容哈希: %q", newHash)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	asset, ok := s.assets[assetID]
	if !ok {
		return fmt.Errorf("媒体资产不存在: %s", assetID)
	}
	// 同内容重复替换幂等返回。
	if asset.Hash == newHash {
		return nil
	}
	s.byHash[asset.Hash] = removeString(s.byHash[asset.Hash], assetID)
	asset.Hash = newHash
	asset.Width, asset.Height, asset.Size = width, height, size
	asset.Variants = variants
	asset.Generation++
	s.byHash[newHash] = append(s.byHash[newHash], assetID)
	return nil
}

// Delete 删除资产：已被引用时强制拦截并返回引用清单（规范 §2 引用追踪与保护）。
func (s *Store) Delete(assetID string) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assets[assetID]; !ok {
		return fmt.Errorf("媒体资产不存在: %s", assetID)
	}
	if refs := s.refs[assetID]; len(refs) > 0 {
		return fmt.Errorf("媒体资产 %s 已被 %d 处引用，禁止删除: %s",
			assetID, len(refs), formatRefs(refs))
	}
	asset := s.assets[assetID]
	s.byHash[asset.Hash] = removeString(s.byHash[asset.Hash], assetID)
	delete(s.assets, assetID)
	delete(s.refs, assetID)
	return nil
}

// RecordRef 登记引用（kind:id 维度幂等）。
func (s *Store) RecordRef(assetID string, ref Reference) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assets[assetID]; !ok {
		return fmt.Errorf("媒体资产不存在: %s", assetID)
	}
	if s.refs[assetID] == nil {
		s.refs[assetID] = map[string]Reference{}
	}
	key := ref.Kind + ":" + ref.ID
	// 幂等登记：已存在时不覆盖（避免后到的空 Title 降级已有引用信息）。
	if _, ok := s.refs[assetID][key]; !ok {
		s.refs[assetID][key] = ref
	}
	return nil
}

// RemoveRef 移除引用记录。
func (s *Store) RemoveRef(assetID string, ref Reference) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.refs[assetID]; m != nil {
		delete(m, ref.Kind+":"+ref.ID)
	}
}

// Refs 返回资产的引用清单。
func (s *Store) Refs(assetID string) (refs []Reference) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.refs[assetID] {
		refs = append(refs, r)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].ID < refs[j].ID
	})
	return refs
}

// SearchFilter 多维搜索过滤（规范 §2 资产管理与检索）。
type SearchFilter struct {
	FileName   string // 文件名模糊匹配
	Type       string // 文件类型精确匹配
	CategoryID string // 分类精确匹配
	Tag        string // 标签精确匹配
	Referenced *bool  // 引用状态过滤：true 已被引用 / false 未被引用
}

// Search 按过滤条件检索资产，按 ID 排序保证确定性。
func (s *Store) Search(f SearchFilter) (results []*Asset) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.assets {
		if f.FileName != "" && !strings.Contains(a.FileName, f.FileName) {
			continue
		}
		if f.Type != "" && a.Type != f.Type {
			continue
		}
		if f.CategoryID != "" && a.CategoryID != f.CategoryID {
			continue
		}
		if f.Tag != "" && !containsString(a.Tags, f.Tag) {
			continue
		}
		if f.Referenced != nil {
			has := len(s.refs[a.ID]) > 0
			if has != *f.Referenced {
				continue
			}
		}
		cp := *a
		results = append(results, &cp)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}

// formatRefs 引用清单格式化（拦截警告展示用）。
func formatRefs(refs map[string]Reference) string {
	parts := make([]string, 0, len(refs))
	keys := make([]string, 0, len(refs))
	for k := range refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := refs[k]
		label := r.Title
		if label == "" {
			label = r.ID
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", label, r.Kind))
	}
	return strings.Join(parts, "、")
}

// containsString 切片包含判断。
func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// removeString 从切片移除首个匹配项。
func removeString(list []string, v string) []string {
	for i, item := range list {
		if item == v {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}
