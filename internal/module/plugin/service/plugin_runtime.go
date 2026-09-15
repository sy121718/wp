package pluginservice

// plugin_runtime.go — 编译装配：启用插件集 → 模板 FS + 组件规格 + 检查器 schema。
// 供 dashboard 预览与 page 构建路径注入（WithPluginResolver + NewCompositeSet）。
// 确定性：同一 (plugin_id, version, manifest) 集构建结果恒定（ListEnabled 字典序）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/plugincomp"
	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginmodel "go_wp/internal/module/plugin/model"
	"go_wp/internal/templates"
)

// EnabledAssembly 构建启用插件的编译装配素材（带进程内缓存，审计 PERF-006）。
//
// 改造前每次页面编译都要走一遍：查库（含 manifest 大字段）→ 逐个 os.Stat 插件目录
// → 解析 manifest → 读 assets/*.css；下游还要用同一批插件重建 Jet Set（把插件模板
// 全部重新解析一遍）。构建一批页面 = 把这套重复 N 遍，而启用集在两次构建之间几乎不变。
//
// 现在按「启用集指纹」缓存：指纹只由轻量列 + 存储目录 mtime 算出，命中即直接返回
// 同一份 Assembly。插件启停 / 升级 / 重装都会改变指纹，因此不需要人工失效入口
// （也就不会出现「改了插件但缓存没失效」这种静默状态）。
//
// 返回的对象是**共享只读**：构建层只读它（PluginFS / Specs / ExtraCSS / 各摘要切片
// 都是按值消费），调用方不得就地修改 —— 需要变体请构造新对象。
func (s *Service) EnabledAssembly(ctx context.Context) (asm *plugincontract.Assembly, err error) {
	rows, err := s.m.ListEnabledFingerprint(ctx)
	if err != nil {
		return nil, err
	}
	fp := assemblyFingerprint(rows)
	if cached := s.cachedAssembly(fp); cached != nil {
		return cached, nil
	}
	asm, err = s.buildAssembly(ctx, fp)
	if err != nil {
		return nil, err
	}
	s.storeAssembly(fp, asm)
	return asm, nil
}

// buildAssembly 真正的构建（只在缓存未命中时执行）：取启用插件全行并逐个读磁盘。
func (s *Service) buildAssembly(ctx context.Context, fingerprint string) (asm *plugincontract.Assembly, err error) {
	rows, err := s.m.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	asm = &plugincontract.Assembly{
		Fingerprint:      fingerprint,
		PluginFS:         make([]templates.PluginFS, 0, len(rows)),
		Specs:            make(map[string]*core.PluginComponentSpec),
		InspectorSchemas: make(map[string][]byte),
		Components:       make([]plugindto.ComponentSummary, 0),
		Presets:          make([]plugindto.PresetSummary, 0),
	}
	for _, row := range rows {
		// 存储目录缺失（被手动清理）→ 跳过该插件并保持注册行（管理员可重装）。
		if st, serr := os.Stat(row.StoragePath); serr != nil || !st.IsDir() {
			continue
		}
		manifest, perr := plugincomp.ParseManifest(row.Manifest)
		if perr != nil {
			continue // 注册时已校验；此处防御（manifest 篡改）静默跳过
		}
		asm.PluginFS = append(asm.PluginFS, templates.PluginFS{
			ID: manifest.ID, FS: os.DirFS(row.StoragePath),
		})
		for t, spec := range plugincomp.BuildSpecs(manifest) {
			asm.Specs[t] = spec
		}
		for t, data := range plugincomp.InspectorSchema(manifest) {
			asm.InspectorSchemas[t] = data
		}
		for _, c := range manifest.Components {
			asm.Components = append(asm.Components, plugindto.ComponentSummary{
				Type:  plugincomp.TypeOf(manifest.ID, c.Name),
				Label: c.Label,
				Hint:  orDefault(c.Hint, "插件组件"),
				Props: defaultProps(c.Props),
			})
		}
		// 区块预设：保持 manifest 声明顺序；跨插件按 ListEnabled 字典序自然有序。
		for _, p := range manifest.Presets {
			asm.Presets = append(asm.Presets, plugindto.PresetSummary{
				ID:        p.ID,
				Label:     p.Label,
				Category:  p.Category,
				Thumbnail: p.Thumbnail,
				Document:  p.Document,
			})
		}
		// 插件静态样式（docs/06 §5.1 资产规范）：assets/*.css 按文件名序拼接，
		// 构建期注入产物主 CSS 之后。文件缺失/目录缺失 = 无样式，静默跳过。
		if css := pluginExtraCSS(manifest.ID, row.StoragePath); css != "" {
			asm.ExtraCSS = append(asm.ExtraCSS, css)
		}
	}
	// 组件摘要按类型排序（palette 注入确定性）。
	slices.SortFunc(asm.Components, func(a, b plugindto.ComponentSummary) int {
		return strings.Compare(a.Type, b.Type)
	})
	return asm, nil
}

// defaultProps 组件插入时的初始 props（manifest 各控件 default）。
func defaultProps(props map[string]plugincomp.PropControl) map[string]any {
	out := make(map[string]any, len(props))
	for k, ctl := range props {
		if ctl.Default != nil {
			out[k] = ctl.Default
		}
	}
	return out
}

// orDefault 空串兜底。
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// cssImportRe 匹配 @import 规则（大小写不敏感，覆盖 @import url(...) 与 @import "..."）。
var cssImportRe = regexp.MustCompile(`(?i)@import[^;]*;?`)

// pluginExtraCSS 读取插件包 assets/*.css 并按文件名序拼接（含来源注释头）。
// 清洗 </style 防止逃逸产物 <style> 块（管理员级信任仍做防御性清洗）。
// 确定性：文件名序 + 拼接顺序固定，同一插件版本恒同字节。
func pluginExtraCSS(pluginID, storagePath string) string {
	entries, err := os.ReadDir(filepath.Join(storagePath, "assets"))
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		data, rerr := os.ReadFile(filepath.Join(storagePath, "assets", name))
		if rerr != nil {
			continue // 单文件读取失败跳过，不阻断其他资产
		}
		css := strings.ReplaceAll(string(data), "</style", "")
		// 禁 @import：外部样式引用构成数据外泄/追踪通道（插件为管理员级信任，
		// 仍做纵深防御；站内资产请用 <link> 由平台统一管理）。
		css = cssImportRe.ReplaceAllString(css, "")
		sb.WriteString("/* plugin:" + pluginID + ":" + name + " */\n")
		sb.WriteString(css)
		sb.WriteString("\n")
	}
	return sb.String()
}

// assemblyFingerprint 计算启用集指纹（审计 PERF-006）。
//
// 参与计算的每一项都是「变了就必须重建」的：
//   - plugin_id / version：换了插件或换了版本；
//   - updated_at：启停与升级都会更新该行；
//   - storage_path：注册路径被改过；
//   - 存储目录 mtime：**同版本重装**（覆盖目录内容）时注册行未必变化，
//     目录 mtime 是这里唯一能观察到它的信号。
//
// 刻意不读 manifest 字节与 assets/*.css：那正是缓存要省掉的开销。
// 代价是「有人手工改插件目录里的文件」这种越权操作可能不改变指纹 ——
// 插件目录由平台管理（安装/卸载都走注册表），这属于可接受的信任边界。
func assemblyFingerprint(rows []pluginmodel.EnabledFingerprintRow) string {
	var sb strings.Builder
	for _, r := range rows {
		sb.WriteString(r.PluginID)
		sb.WriteByte('|')
		sb.WriteString(r.Version)
		sb.WriteByte('|')
		sb.WriteString(r.StoragePath)
		sb.WriteByte('|')
		sb.WriteString(strconv.FormatInt(r.UpdatedAt.UTC().UnixNano(), 10))
		sb.WriteByte('|')
		if st, serr := os.Stat(r.StoragePath); serr == nil {
			sb.WriteString(strconv.FormatInt(st.ModTime().UTC().UnixNano(), 10))
		}
		sb.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// cachedAssembly 命中则返回缓存对象（只在锁内读写字段，构建过程不在锁内）。
func (s *Service) cachedAssembly(fp string) *plugincontract.Assembly {
	s.asmMu.Lock()
	defer s.asmMu.Unlock()
	if s.asmFingerprint == fp && s.asmCache != nil {
		return s.asmCache
	}
	return nil
}

// storeAssembly 写入缓存。
//
// 只保留最新一份而不是留几个版本：启用集切换是低频事件，留下旧集合会让
// 「旧插件集悄然复活」变得难以察觉（切回去不报错、但用户以为改动已生效）。
func (s *Service) storeAssembly(fp string, asm *plugincontract.Assembly) {
	s.asmMu.Lock()
	defer s.asmMu.Unlock()
	s.asmFingerprint, s.asmCache = fp, asm
}
