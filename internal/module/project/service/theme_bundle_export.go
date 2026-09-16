package projectservice

// theme_bundle_export.go — 主题包导出（审计 VIS-014 第 2 步）。
//
// 导出内容 = 设计令牌 + 槽位预设 + 引用到的块文档（闭包）+ 可选页面文档 + 媒体引用清单，
// 打包成一个 zip。引用闭包按「谁能引用块」全量展开：主题槽位（header/footer/slots）与
// 页面文档里的 globalref 节点都是种子 —— 只导出槽位那几个块会漏掉页面里引用的块，
// 导入后表现为「有些页面缺一块内容」。

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"go_wp/internal/builder"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"gorm.io/gorm"
)

// 主题设置的两个分区与只读元数据（与 builder/theme_schema.go 的存储规范一致）。
//
// 导出时按这个清单切分：结构分区（headerBlockId/footerBlockId/slots）不走 tokens.json
// （它引用块，导入时必须换成新 id），只读元数据（themeId）不进包（它是存储层注入的标识，
// 复制到另一个工程只会让两个工程共用同一个主题标识）。
var (
	themeStructureKeys = map[string]struct{}{
		"headerBlockId": {}, "footerBlockId": {}, "slots": {},
	}
	themeReadonlyKeys = map[string]struct{}{"themeId": {}}
)

// bundleBlockAsset 导出期的块资产（Key 为包内 key，Document 已改写为 key 引用）。
type bundleBlockAsset struct {
	Key       string
	Name      string
	Kind      string
	Category  string
	ReuseMode string
	Document  json.RawMessage
	Refs      []string
}

// bundlePageAsset 导出期的页面资产。
type bundlePageAsset struct {
	Key      string
	Kind     string
	Path     string
	Document json.RawMessage
}

// bundleMediaAsset 导出期的媒体条目（Data 非空表示随包内嵌）。
type bundleMediaAsset struct {
	Entry themeBundleMediaEntry
	Data  []byte
}

// ExportThemeBundle 导出主题包。
func (s *Service) ExportThemeBundle(ctx context.Context, req *projectdto.ThemeBundleExportReq) (res *projectdto.ThemeBundleExportResp, err error) {
	if req == nil || strings.TrimSpace(req.ThemeID) == "" {
		return nil, ErrThemeNotFound
	}
	if s.assets == nil {
		return nil, ErrThemeBundlePortUnavailable
	}
	mediaMode := strings.TrimSpace(req.Media)
	switch mediaMode {
	case "":
		mediaMode = projectdto.ThemeBundleMediaEmbed
	case projectdto.ThemeBundleMediaEmbed, projectdto.ThemeBundleMediaDeclare:
	default:
		return nil, ErrInvalidParam
	}
	entity, gerr := s.model.GetTheme(ctx, req.ThemeID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, ErrThemeNotFound
		}
		return nil, gerr
	}

	warnings := make([]string, 0, 4)
	settings, derr := decodeBundleJSON(entity.Settings)
	if derr != nil {
		return nil, ErrInvalidThemeSettings
	}
	settingsMap, _ := settings.(map[string]any)
	if settingsMap == nil {
		settingsMap = map[string]any{}
	}
	tokens := make(map[string]any, len(settingsMap))
	for k, v := range settingsMap {
		if _, isStructure := themeStructureKeys[k]; isStructure {
			continue
		}
		if _, isReadonly := themeReadonlyKeys[k]; isReadonly {
			continue
		}
		tokens[k] = v
	}
	tokensRaw, terr := json.Marshal(tokens)
	if terr != nil {
		return nil, ErrThemeBundleTokensInvalid
	}
	// 令牌必须过与站点主题同一套白名单校验：包里带一份非法 CSS 值等于把注入带进目标环境。
	if _, verr := builder.ParseThemeSettings(tokensRaw); verr != nil {
		return nil, ErrThemeBundleTokensInvalid
	}
	slots := extractThemeSlots(settingsMap)

	// 可选页面：页面属站点内容，默认不带（主题包应能在另一站点直接铺开而不覆盖其内容）。
	var pages []bundlePageAsset
	if req.WithPages {
		list, perr := s.assets.ListPages(ctx, entity.ProjectID, entity.ID)
		if perr != nil {
			// 基础设施故障原样上抛（与项目既有口径一致），不吞成业务错误。
			return nil, perr
		}
		pages, err = buildBundlePages(list, &warnings)
		if err != nil {
			return nil, err
		}
	}
	if len(pages) > themeBundleMaxPages {
		return nil, ErrThemeBundleTooLarge
	}

	// 块引用闭包：槽位 + 页面文档里的块引用都是种子。
	seeds := slotSeedIDs(slots)
	for _, p := range pages {
		doc, perr := decodeBundleJSON(p.Document)
		if perr != nil {
			return nil, ErrThemeBundleManifestInvalid
		}
		seeds = append(seeds, collectBundleBlockRefs(doc)...)
	}
	assets, keyByID, cerr := s.collectBundleBlocks(ctx, entity.ProjectID, seeds, &warnings)
	if cerr != nil {
		return nil, cerr
	}
	// 改写：块文档里的块引用与页面文档里的块引用都换成包内 key。
	for i := range assets {
		rewritten, refs, werr := rewriteBundleDocument(assets[i].Document, keyByID)
		if werr != nil {
			return nil, werr
		}
		assets[i].Document = rewritten
		assets[i].Refs = refs
	}
	for i := range pages {
		rewritten, _, werr := rewriteBundleDocument(pages[i].Document, keyByID)
		if werr != nil {
			return nil, werr
		}
		pages[i].Document = rewritten
	}
	slotPreset := rewriteSlotsToKeys(slots, keyByID, &warnings)

	// 媒体依赖：扫描块与页面文档里出现的 /storage 引用。
	docRefs := make([]themeBundleDocRef, 0, len(assets)+len(pages))
	for _, a := range assets {
		docRefs = append(docRefs, themeBundleDocRef{Raw: a.Document, Label: "block:" + a.Key})
	}
	for _, p := range pages {
		docRefs = append(docRefs, themeBundleDocRef{Raw: p.Document, Label: "page:" + p.Key})
	}
	mediaAssets := buildBundleMediaAssets(collectBundleMediaRefs(docRefs), mediaMode, &warnings)

	manifest := &themeBundleManifest{
		Format:        ThemeBundleFormat,
		SchemaVersion: ThemeBundleSchemaVersion,
		Generator:     themeBundleGenerator,
		CreatedAt:     themeBundleManifestCreatedAt(time.Now()),
		Source:        themeBundleSource{ProjectID: entity.ProjectID, ThemeID: entity.ID, ThemeName: entity.Name},
		Theme:         themeBundleThemeMeta{Name: entity.Name, Slots: slotPreset},
	}
	for i := range assets {
		manifest.Blocks = append(manifest.Blocks, themeBundleBlockEntry{
			Key: assets[i].Key, Name: assets[i].Name, Kind: assets[i].Kind,
			Category: assets[i].Category, ReuseMode: assets[i].ReuseMode, Refs: assets[i].Refs,
		})
	}
	for i := range pages {
		manifest.Pages = append(manifest.Pages, themeBundlePageEntry{
			Key: pages[i].Key, Kind: pages[i].Kind, Path: pages[i].Path,
		})
	}
	embedded := 0
	for _, m := range mediaAssets {
		if len(m.Data) > 0 {
			embedded++
		}
		manifest.Media = append(manifest.Media, m.Entry)
	}
	manifestRaw, merr := marshalThemeBundleManifest(manifest)
	if merr != nil {
		return nil, ErrThemeBundleManifestInvalid
	}

	zw, buf := newThemeBundleZipWriter()
	if aerr := zw.add(themeBundleManifestName, manifestRaw); aerr != nil {
		return nil, aerr
	}
	if aerr := zw.add(themeBundleTokensName, append(tokensRaw, bundleNewline)); aerr != nil {
		return nil, aerr
	}
	if !slotPreset.isEmpty() {
		slotRaw, serr := json.MarshalIndent(slotPreset, "", "  ")
		if serr != nil {
			return nil, ErrThemeBundleManifestInvalid
		}
		if aerr := zw.add(themeBundleSlotsName, append(slotRaw, bundleNewline)); aerr != nil {
			return nil, aerr
		}
	}
	for i := range assets {
		raw, jerr := json.MarshalIndent(themeBundleBlockFile{
			Key: assets[i].Key, Name: assets[i].Name, Kind: assets[i].Kind,
			Category: assets[i].Category, ReuseMode: assets[i].ReuseMode, Document: assets[i].Document,
		}, "", "  ")
		if jerr != nil {
			return nil, ErrThemeBundleManifestInvalid
		}
		if aerr := zw.add(themeBundleBlocksPrefix+assets[i].Key+".json", append(raw, bundleNewline)); aerr != nil {
			return nil, aerr
		}
	}
	for i := range pages {
		raw, jerr := json.MarshalIndent(themeBundlePageFile{
			Key: pages[i].Key, Kind: pages[i].Kind, Path: pages[i].Path, Document: pages[i].Document,
		}, "", "  ")
		if jerr != nil {
			return nil, ErrThemeBundleManifestInvalid
		}
		if aerr := zw.add(themeBundlePagesPrefix+pages[i].Key+".json", append(raw, bundleNewline)); aerr != nil {
			return nil, aerr
		}
	}
	for _, m := range mediaAssets {
		if len(m.Data) == 0 {
			continue
		}
		if aerr := zw.add(m.Entry.BundlePath, m.Data); aerr != nil {
			return nil, aerr
		}
	}
	if cerr := zw.close(); cerr != nil {
		return nil, cerr
	}

	res = &projectdto.ThemeBundleExportResp{
		FileName:      cleanBundleName(entity.Name),
		Bytes:         buf.Bytes(),
		ThemeID:       entity.ID,
		BlockCount:    len(assets),
		PageCount:     len(pages),
		MediaEmbedded: embedded,
		MediaDeclared: len(mediaAssets) - embedded,
		Warnings:      warnings,
	}
	return res, nil
}

// collectBundleBlocks 从种子块 id 出发做引用闭包（广度优先，带已访问集合防环）。
func (s *Service) collectBundleBlocks(ctx context.Context, projectID string, seeds []string, warnings *[]string) (assets []bundleBlockAsset, keyByID map[string]string, err error) {
	keyByID = map[string]string{}
	visited := map[string]struct{}{}
	queue := make([]string, 0, len(seeds))
	for _, id := range seeds {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			queue = append(queue, trimmed)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, done := visited[id]; done {
			continue
		}
		visited[id] = struct{}{}
		blk, gerr := s.assets.GetBlock(ctx, projectID, id)
		if gerr != nil {
			if errors.Is(gerr, ErrThemeBundleAssetMissing) {
				*warnings = append(*warnings, "引用的块不存在，已跳过："+id)
				continue
			}
			return nil, nil, gerr
		}
		if blk == nil {
			*warnings = append(*warnings, "引用的块不存在，已跳过："+id)
			continue
		}
		if len(assets) >= themeBundleMaxBlocks {
			return nil, nil, ErrThemeBundleTooLarge
		}
		doc, derr := decodeBundleJSON(blk.Document)
		if derr != nil {
			// 块文档坏掉不能静默跳过：导出一个缺块的「主题」比拒绝导出更坏。
			return nil, nil, ErrThemeBundleManifestInvalid
		}
		key := bundleKey("b", len(assets)+1)
		keyByID[id] = key
		assets = append(assets, bundleBlockAsset{
			Key: key, Name: blk.Name, Kind: blk.Kind, Category: blk.Category,
			ReuseMode: blk.ReuseMode, Document: blk.Document, Refs: collectBundleBlockRefs(doc),
		})
		queue = append(queue, collectBundleBlockRefs(doc)...)
	}
	return assets, keyByID, nil
}

// rewriteBundleDocument 把文档里的块引用换成目标映射，并返回改写后引用的（映射内）值。
func rewriteBundleDocument(raw json.RawMessage, mapping map[string]string) (out json.RawMessage, refs []string, err error) {
	doc, derr := decodeBundleJSON(raw)
	if derr != nil {
		return nil, nil, ErrThemeBundleManifestInvalid
	}
	rewritten, werr := encodeBundleJSON(rewriteBundleBlockRefs(doc, mapping))
	if werr != nil {
		return nil, nil, ErrThemeBundleManifestInvalid
	}
	for _, r := range collectBundleBlockRefs(doc) {
		if key, hit := mapping[r]; hit {
			refs = append(refs, key)
		}
	}
	sort.Strings(refs)
	return rewritten, refs, nil
}

// buildBundlePages 把页面投影转成包内页面资产（按 path 排序，key 顺序因此可复现）。
func buildBundlePages(list []projectcontract.ThemeBundlePage, warnings *[]string) (pages []bundlePageAsset, err error) {
	sorted := make([]projectcontract.ThemeBundlePage, 0, len(list))
	sorted = append(sorted, list...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].DraftPath < sorted[j].DraftPath })
	for i, p := range sorted {
		if strings.TrimSpace(p.DraftPath) == "" {
			*warnings = append(*warnings, "页面缺少路径，已跳过："+p.ID)
			continue
		}
		if len(p.Document) == 0 {
			*warnings = append(*warnings, "页面没有文档内容，已跳过："+p.DraftPath)
			continue
		}
		pages = append(pages, bundlePageAsset{
			Key:      bundleKey("p", i+1),
			Kind:     p.Kind,
			Path:     p.DraftPath,
			Document: p.Document,
		})
	}
	return pages, nil
}

// extractThemeSlots 从主题设置里取槽位绑定。
func extractThemeSlots(settings map[string]any) themeBundleSlots {
	slots := themeBundleSlots{
		Header: stringField(settings, "headerBlockId"),
		Footer: stringField(settings, "footerBlockId"),
	}
	if obj, ok := settings["slots"].(map[string]any); ok {
		m := make(map[string]string, len(obj))
		for k, v := range obj {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				m[k] = strings.TrimSpace(s)
			}
		}
		if len(m) > 0 {
			slots.Slots = m
		}
	}
	return slots
}

// slotSeedIDs 槽位绑定的块 id（顺序固定：header → footer → slots 按槽位名）。
func slotSeedIDs(slots themeBundleSlots) []string {
	out := make([]string, 0, len(slots.Slots)+2)
	if slots.Header != "" {
		out = append(out, slots.Header)
	}
	if slots.Footer != "" {
		out = append(out, slots.Footer)
	}
	for _, name := range sortedKeys(slots.Slots) {
		out = append(out, slots.Slots[name])
	}
	return out
}

// rewriteSlotsToKeys 把槽位绑定里的块 id 换成包内 key；映射不到的（悬空引用）丢弃并记提示。
func rewriteSlotsToKeys(slots themeBundleSlots, keyByID map[string]string, warnings *[]string) themeBundleSlots {
	out := themeBundleSlots{}
	mapOne := func(id string) string {
		if id == "" {
			return ""
		}
		if key, hit := keyByID[id]; hit {
			return key
		}
		*warnings = append(*warnings, "槽位绑定的块不在包内（引用已剔除）："+id)
		return ""
	}
	out.Header = mapOne(slots.Header)
	out.Footer = mapOne(slots.Footer)
	if len(slots.Slots) > 0 {
		slotsMap := make(map[string]string, len(slots.Slots))
		for _, name := range sortedKeys(slots.Slots) {
			if key := mapOne(slots.Slots[name]); key != "" {
				slotsMap[name] = key
			}
		}
		if len(slotsMap) > 0 {
			out.Slots = slotsMap
		}
	}
	return out
}

// buildBundleMediaAssets 按媒体策略生成清单条目：能读到字节的内嵌，读不到的作为缺失声明。
func buildBundleMediaAssets(refs []themeBundleMediaRef, mediaMode string, warnings *[]string) (assets []bundleMediaAsset) {
	root := themeBundleMediaRoot()
	for i, ref := range refs {
		entry := themeBundleMediaEntry{
			Key:          bundleKey("m", i+1),
			URL:          ref.URL,
			Path:         ref.Path,
			ReferencedBy: ref.ReferencedBy,
		}
		if mediaMode == projectdto.ThemeBundleMediaEmbed {
			sum, size, data, ok := mediaFileSHA256(root, ref.Path)
			if ok {
				entry.Embedded = true
				entry.SHA256 = sum
				entry.Size = size
				entry.BundlePath = themeBundleMediaPrefix + themeBundleMediaFileName(entry.Key, ref.Path)
				assets = append(assets, bundleMediaAsset{Entry: entry, Data: data})
				continue
			}
			entry.Reason = themeBundleReasonNotFoundInSource
			*warnings = append(*warnings, "媒体不在本地上传目录，已作为缺失声明："+ref.URL)
		} else {
			entry.Reason = themeBundleReasonDeclaredOnly
		}
		assets = append(assets, bundleMediaAsset{Entry: entry})
	}
	return assets
}

// stringField 读取字符串字段（缺失/非字符串返回空串）。
func stringField(obj map[string]any, key string) string {
	if v, ok := obj[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
