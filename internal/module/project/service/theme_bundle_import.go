package projectservice

// theme_bundle_import.go — 主题包导入（审计 VIS-014 第 3、4 步）。
//
// 导入把包内的 key 全部换成**新分配的 id**：
//   - 块：逐个经 block 契约新建（id 由 block 模块分配），包内旧 id 根本不存在，无从沿用；
//   - 页面：同样新建，槽位与页面文档里的块引用统一指向新块 id；
//   - 主题：新建（重名自动加后缀并如实上报，不静默改名）。
//
// 不是原子操作：跨模块没有共享事务（block / page / project 各自的表），所以顺序刻意
// 排成「失败概率最低的先做、且后一步失败不会让前一步变成不可用」：
// 解包 → 校验 → 媒体落盘 → 块 → 页面 → 主题 → 激活。任何一步失败都返回明确错误，
// 已落盘的媒体文件不构成错误状态（引用仍可用），已在报告 omission 之外的部分不静默掩盖。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go_wp/internal/builder"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"gorm.io/gorm"
)

// ImportThemeBundle 导入主题包。
func (s *Service) ImportThemeBundle(ctx context.Context, req *projectdto.ThemeBundleImportReq) (res *projectdto.ThemeBundleImportResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrThemeProjectIDEmpty
	}
	if len(req.Zip) == 0 {
		return nil, ErrThemeBundleFileRequired
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if _, perr := s.model.GetByID(ctx, projectID); perr != nil {
		if errors.Is(perr, gorm.ErrRecordNotFound) {
			return nil, ErrProjectNotFound
		}
		return nil, perr
	}

	files, zerr := readThemeBundleZip(req.Zip)
	if zerr != nil {
		return nil, zerr
	}
	manifest, merr := parseThemeBundleManifest(files[themeBundleManifestName])
	if merr != nil {
		return nil, merr
	}

	// 令牌：必需文件 + 与站点主题同一套白名单校验。
	tokensRaw, ok := files[themeBundleTokensName]
	if !ok {
		return nil, ErrThemeBundleManifestInvalid
	}
	tokensNode, derr := decodeBundleJSON(tokensRaw)
	if derr != nil {
		return nil, ErrThemeBundleTokensInvalid
	}
	tokensMap, ok := tokensNode.(map[string]any)
	if !ok {
		return nil, ErrThemeBundleTokensInvalid
	}
	if _, verr := builder.ParseThemeSettings(tokensRaw); verr != nil {
		return nil, ErrThemeBundleTokensInvalid
	}

	warnings := make([]string, 0, 4)
	if _, hasPreview := files[themeBundlePreviewName]; hasPreview {
		warnings = append(warnings, "包内包含预览图，本版导入流程不消费该文件（格式已预留）")
	}
	slots := readBundleSlots(files, manifest)

	// 媒体：先落盘/核对，任何「引用不到」的条目都进缺失清单（不静默留死链）。
	mediaReport := s.applyBundleMedia(files, manifest, &warnings)

	// 块：包内完整性校验 + 拓扑排序（被引用者先建），再逐个新建拿新 id。
	blockFiles, beErr := readBundleBlockFiles(files, manifest)
	if beErr != nil {
		return nil, beErr
	}
	var importedBlocks []projectdto.ThemeBundleImportedBlock
	idByKey := map[string]string{}
	if len(blockFiles) > 0 {
		if s.assets == nil {
			return nil, ErrThemeBundlePortUnavailable
		}
		order, oerr := orderBundleBlocks(blockFiles)
		if oerr != nil {
			return nil, oerr
		}
		for _, key := range order {
			bf := blockFiles[key]
			doc, derr := decodeBundleJSON(bf.Document)
			if derr != nil {
				return nil, ErrThemeBundleManifestInvalid
			}
			// 此时被引用的块已建好，映射里已有它们的新 id。
			rewritten, werr := encodeBundleJSON(rewriteBundleBlockRefs(doc, idByKey))
			if werr != nil {
				return nil, ErrThemeBundleManifestInvalid
			}
			newID, cerr := s.assets.CreateBlock(ctx, &projectcontract.ThemeBundleBlockCreate{
				ProjectID: projectID,
				Name:      bf.Name,
				Kind:      bf.Kind,
				Category:  bf.Category,
				ReuseMode: bf.ReuseMode,
				Document:  rewritten,
			})
			if cerr != nil {
				return nil, cerr
			}
			idByKey[key] = newID
			importedBlocks = append(importedBlocks, projectdto.ThemeBundleImportedBlock{
				Key: key, ID: newID, Name: bf.Name, Kind: bf.Kind, ReuseMode: bf.ReuseMode,
			})
		}
	}

	// 页面：路径已在目标工程占用时**跳过并上报**，不做静默改名（改名会悄悄换掉站点的 URL 设计）。
	var importedPages []projectdto.ThemeBundleImportedPage
	var skippedPages []projectdto.ThemeBundlePageSkip
	if req.CreatePages {
		pageFiles, pErr := readBundlePageFiles(files, manifest)
		if pErr != nil {
			return nil, pErr
		}
		if len(pageFiles) > 0 && s.assets == nil {
			return nil, ErrThemeBundlePortUnavailable
		}
		for _, key := range sortedPageKeys(pageFiles) {
			pf := pageFiles[key]
			taken, tkErr := s.assets.PagePathTaken(ctx, projectID, pf.Path)
			if tkErr != nil {
				return nil, tkErr
			}
			if taken {
				skippedPages = append(skippedPages, projectdto.ThemeBundlePageSkip{
					Key: key, Path: pf.Path, Reason: "路径已被目标工程的页面占用",
				})
				continue
			}
			doc, derr := decodeBundleJSON(pf.Document)
			if derr != nil {
				return nil, ErrThemeBundleManifestInvalid
			}
			rewritten, werr := encodeBundleJSON(rewriteBundleBlockRefs(doc, idByKey))
			if werr != nil {
				return nil, ErrThemeBundleManifestInvalid
			}
			newID, cerr := s.assets.CreatePage(ctx, &projectcontract.ThemeBundlePageCreate{
				ProjectID: projectID,
				Kind:      pf.Kind,
				DraftPath: pf.Path,
				Document:  rewritten,
			})
			if cerr != nil {
				return nil, cerr
			}
			importedPages = append(importedPages, projectdto.ThemeBundleImportedPage{
				Key: key, ID: newID, Path: pf.Path,
			})
		}
	} else if len(manifest.Pages) > 0 {
		warnings = append(warnings, "包内包含页面，但本次未选择导入页面")
	}

	// 主题：名称冲突自动加后缀（如实上报），设置由令牌 + 槽位快照组成。
	desired := strings.TrimSpace(req.Name)
	if desired == "" {
		desired = manifest.Theme.Name
	}
	name, adjusted, nerr := s.resolveBundleThemeName(ctx, projectID, desired)
	if nerr != nil {
		return nil, nerr
	}
	settingsMap := make(map[string]any, len(tokensMap)+3)
	for k, v := range tokensMap {
		settingsMap[k] = v
	}
	structure := rewriteSlotsFromKeys(slots, idByKey, &warnings)
	if structure.Header != "" {
		settingsMap["headerBlockId"] = structure.Header
	}
	if structure.Footer != "" {
		settingsMap["footerBlockId"] = structure.Footer
	}
	if len(structure.Slots) > 0 {
		settingsMap["slots"] = structure.Slots
	}
	settingsRaw, serr := json.Marshal(settingsMap)
	if serr != nil {
		return nil, ErrInvalidThemeSettings
	}
	theme, cerr := s.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: name, Settings: settingsRaw,
	})
	if cerr != nil {
		return nil, cerr
	}
	if req.Activate {
		if aerr := s.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: theme.ID}); aerr != nil {
			return nil, aerr
		}
	}
	// 只在真有内嵌媒体时提示：media 模块的表由它自己维护，这里只落文件。
	if mediaReport.Embedded > 0 {
		warnings = append(warnings,
			"内嵌媒体只落盘到 /storage，不登记媒体库附件记录（媒体库属 media 模块，project 不越权写它的表）")
	}

	res = &projectdto.ThemeBundleImportResp{
		ThemeID:       theme.ID,
		ThemeName:     theme.Name,
		RequestedName: desired,
		NameAdjusted:  adjusted,
		Activated:     req.Activate,
		SchemaVersion: manifest.SchemaVersion,
		Blocks:        importedBlocks,
		Pages:         importedPages,
		Media:         mediaReport,
		SkippedPages:  skippedPages,
		Warnings:      warnings,
	}
	return res, nil
}

// readBundleSlots 读取槽位预设：slots.json 优先，缺失时回退 manifest 里的冗余副本。
func readBundleSlots(files map[string][]byte, manifest *themeBundleManifest) themeBundleSlots {
	raw, ok := files[themeBundleSlotsName]
	if !ok {
		return manifest.Theme.Slots
	}
	slots := themeBundleSlots{}
	if err := json.Unmarshal(raw, &slots); err != nil {
		return manifest.Theme.Slots
	}
	return slots
}

// readBundleBlockFiles 读取包内块文档，并校验 manifest 声明与包内容一一对应。
func readBundleBlockFiles(files map[string][]byte, manifest *themeBundleManifest) (map[string]themeBundleBlockFile, error) {
	out := make(map[string]themeBundleBlockFile, len(manifest.Blocks))
	for _, entry := range manifest.Blocks {
		raw, ok := files[themeBundleBlocksPrefix+entry.Key+".json"]
		if !ok {
			// 声明了却不在包里 = 包不完整，拒绝导入（而不是导出一个缺块的主题）。
			return nil, fmt.Errorf("%w: %s", ErrThemeBundleBlockMissing, entry.Key)
		}
		bf := themeBundleBlockFile{}
		if err := json.Unmarshal(raw, &bf); err != nil {
			return nil, ErrThemeBundleManifestInvalid
		}
		if len(bf.Document) == 0 {
			return nil, ErrThemeBundleManifestInvalid
		}
		if bf.Key == "" {
			bf.Key = entry.Key
		}
		if bf.Kind == "" {
			bf.Kind = entry.Kind
		}
		if bf.ReuseMode == "" {
			bf.ReuseMode = entry.ReuseMode
		}
		out[entry.Key] = bf
	}
	return out, nil
}

// readBundlePageFiles 读取包内页面文档。
func readBundlePageFiles(files map[string][]byte, manifest *themeBundleManifest) (map[string]themeBundlePageFile, error) {
	out := make(map[string]themeBundlePageFile, len(manifest.Pages))
	for _, entry := range manifest.Pages {
		raw, ok := files[themeBundlePagesPrefix+entry.Key+".json"]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrThemeBundleBlockMissing, entry.Key)
		}
		pf := themeBundlePageFile{}
		if err := json.Unmarshal(raw, &pf); err != nil {
			return nil, ErrThemeBundleManifestInvalid
		}
		if len(pf.Document) == 0 || strings.TrimSpace(pf.Path) == "" {
			return nil, ErrThemeBundleManifestInvalid
		}
		out[entry.Key] = pf
	}
	return out, nil
}

// sortedPageKeys 页面 key 的有序列表（确定性：按包内路径排序后 key 也是有序的）。
func sortedPageKeys(files map[string]themeBundlePageFile) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// orderBundleBlocks 拓扑排序：被引用的块先创建（否则创建某块时拿不到它引用块的 id）。
//
// 引用关系取自包内文档本身的 key 引用；引用包内没有的 key → 包不完整，拒绝；
// 成环 → 拒绝（环上任何顺序都在引用一个尚不存在的 id，导入结果必然自相矛盾）。
func orderBundleBlocks(blocks map[string]themeBundleBlockFile) (order []string, err error) {
	deps := make(map[string][]string, len(blocks))
	for key, bf := range blocks {
		doc, derr := decodeBundleJSON(bf.Document)
		if derr != nil {
			return nil, ErrThemeBundleManifestInvalid
		}
		refs := collectBundleBlockRefs(doc)
		for _, r := range refs {
			if _, ok := blocks[r]; !ok {
				return nil, fmt.Errorf("%w: %s -> %s", ErrThemeBundleBlockMissing, key, r)
			}
		}
		deps[key] = refs
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	state := make(map[string]int, len(blocks))
	keys := make([]string, 0, len(blocks))
	for k := range blocks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var visit func(key string) error
	visit = func(key string) error {
		switch state[key] {
		case black:
			return nil
		case gray:
			return ErrThemeBundleBlockCycle
		}
		state[key] = gray
		for _, dep := range deps[key] {
			if derr := visit(dep); derr != nil {
				return derr
			}
		}
		state[key] = black
		order = append(order, key)
		return nil
	}
	for _, key := range keys {
		if derr := visit(key); derr != nil {
			return nil, derr
		}
	}
	return order, nil
}

// applyBundleMedia 处理包内媒体：内嵌落盘 / 目标已有 / 冲突 / 缺失清单。
func (s *Service) applyBundleMedia(files map[string][]byte, manifest *themeBundleManifest, warnings *[]string) (report projectdto.ThemeBundleMediaReport) {
	root := themeBundleMediaRoot()
	report.Declared = len(manifest.Media)
	for _, entry := range manifest.Media {
		item := projectdto.ThemeBundleMediaItem{
			Key: entry.Key, URL: entry.URL, Path: entry.Path,
			SHA256: entry.SHA256, Size: entry.Size, ReferencedBy: entry.ReferencedBy,
		}
		if entry.Embedded && entry.BundlePath != "" {
			data, ok := files[entry.BundlePath]
			if !ok {
				// 声明内嵌却不在包里 = 包损坏，按缺失处理并如实上报。
				*warnings = append(*warnings, "包内声明的内嵌媒体缺失："+entry.BundlePath)
				report.Missing = append(report.Missing, projectdto.ThemeBundleMissingMedia{
					Key: entry.Key, URL: entry.URL, Path: entry.Path,
					Reason: themeBundleReasonNotInBundle, WillDeadLink: true, ReferencedBy: entry.ReferencedBy,
				})
				continue
			}
			action, werr := writeMediaFile(root, entry.Path, data)
			if werr != nil {
				*warnings = append(*warnings, "媒体落盘失败（已计入缺失）："+entry.URL)
				report.Missing = append(report.Missing, projectdto.ThemeBundleMissingMedia{
					Key: entry.Key, URL: entry.URL, Path: entry.Path,
					Reason: themeBundleReasonNotInBundle, WillDeadLink: true, ReferencedBy: entry.ReferencedBy,
				})
				continue
			}
			item.Action = action
			report.Embedded++
			if action == projectdto.ThemeBundleMediaActionConflict {
				report.Conflicts = append(report.Conflicts, item)
				continue
			}
			report.Resolved = append(report.Resolved, item)
			continue
		}
		// 未内嵌：目标环境已有该文件则引用可用，否则进缺失清单。
		sum, size, _, ok := mediaFileSHA256(root, entry.Path)
		if ok {
			item.Action = projectdto.ThemeBundleMediaActionAvailable
			if sum != "" {
				item.SHA256 = sum
			}
			if size > 0 {
				item.Size = size
			}
			report.Resolved = append(report.Resolved, item)
			if entry.SHA256 != "" && entry.SHA256 != sum {
				*warnings = append(*warnings, "目标环境已有同名媒体但内容与包内声明不同："+entry.URL)
			}
			continue
		}
		reason := entry.Reason
		switch reason {
		case themeBundleReasonNotFoundInSource, themeBundleReasonDeclaredOnly, themeBundleReasonNotInBundle:
		default:
			reason = themeBundleReasonNotInBundle
		}
		report.Missing = append(report.Missing, projectdto.ThemeBundleMissingMedia{
			Key: entry.Key, URL: entry.URL, Path: entry.Path,
			Reason: reason, WillDeadLink: true, ReferencedBy: entry.ReferencedBy,
		})
	}
	return report
}

// rewriteSlotsFromKeys 把包内 key 换成新分配的块 id（映射不到的剔除并记提示）。
func rewriteSlotsFromKeys(slots themeBundleSlots, idByKey map[string]string, warnings *[]string) themeBundleSlots {
	out := themeBundleSlots{}
	mapOne := func(key string) string {
		if key == "" {
			return ""
		}
		if id, hit := idByKey[key]; hit {
			return id
		}
		*warnings = append(*warnings, "槽位预设引用的块未导入，绑定已剔除："+key)
		return ""
	}
	out.Header = mapOne(slots.Header)
	out.Footer = mapOne(slots.Footer)
	if len(slots.Slots) > 0 {
		slotsMap := make(map[string]string, len(slots.Slots))
		for _, name := range sortedKeys(slots.Slots) {
			if id := mapOne(slots.Slots[name]); id != "" {
				slotsMap[name] = id
			}
		}
		if len(slotsMap) > 0 {
			out.Slots = slotsMap
		}
	}
	return out
}

// resolveBundleThemeName 解析导入后的主题名：重名就加后缀（上限 50 次尝试），并如实上报。
func (s *Service) resolveBundleThemeName(ctx context.Context, projectID, desired string) (name string, adjusted bool, err error) {
	base := strings.TrimSpace(desired)
	if base == "" {
		base = "导入主题"
	}
	if len([]rune(base)) > 90 {
		base = string([]rune(base)[:90])
	}
	candidate := base
	for i := 2; i <= 50; i++ {
		exists, eerr := s.model.ExistsByName(ctx, projectID, candidate)
		if eerr != nil {
			return "", false, eerr
		}
		if !exists {
			return candidate, candidate != base, nil
		}
		candidate = fmt.Sprintf("%s (导入 %d)", base, i)
	}
	return "", false, ErrThemeDuplicateName
}
