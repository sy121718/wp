package pluginservice

// 未信任输入防线：
//   - zip slip 路径穿越防护（清理后必须仍在目标目录内，拒绝绝对路径/上溯）；
//   - 扩展名白名单（模板/schema/资产，拒绝可执行与未知类型）；
//   - 解包尺寸与条目数上限；
//   - manifest 经 plugincomp.ParseManifest 白名单校验（属性名/选择器/值/控件类型）；
//   - 存储路径由服务端拼接（pluginID/version 来自校验后的 manifest，白名单字符）。

// 每个插件一个独立 PG schema（plugin_<id>），表名零冲突；迁移 SQL 由插件
// zip 的 {Migrations}/ 目录携带（版本化 .sql 文件，按文件名字典序执行），
// 文件内自行包含 CREATE SCHEMA IF NOT EXISTS plugin_<id> + 建表语句——
// 执行器不代写 schema 语句，只负责逐条执行（复用 public/migrations 的
// SplitStatements 拆分多语句，尊重 DO $$ 块 / -- 注释 / 字符串字面量内的分号）。

// 供 dashboard 预览与 page 构建路径注入（WithPluginResolver + NewCompositeSet）。
// 确定性：同一 (plugin_id, version, manifest) 集构建结果恒定（ListEnabled 字典序）。

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/plugincomp"
	"go_wp/internal/module/plugin/contract"
	"go_wp/internal/module/plugin/dto"
	"go_wp/internal/module/plugin/enums"
	"go_wp/internal/module/plugin/model"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
)

// 安装防线常量。
const (
	zipMaxBytes     = 50 << 20 // 50MB 解包总上限
	zipMaxEntries   = 500      // 条目数上限
	zipMaxFileBytes = 10 << 20 // 单文件上限
)

// extWhitelist 解包扩展名白名单（模板/数据/资产/文档）。
var extWhitelist = map[string]bool{
	".jet": true, ".json": true, ".css": true, ".js": true, ".svg": true,
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
	".woff2": true, ".woff": true, ".ttf": true, ".sql": true, ".md": true, ".txt": true,
}

// pluginStorageRoot 插件解包根目录：GO_WP_PLUGIN_ROOT 环境变量可覆盖
// （测试隔离用），默认 public/runtime/plugins（运行时产物区，未挂载任何
// 对外静态路由：/storage 只挂 public/storage，/site 只挂激活产物）。
// 与 pipeline.DefaultArtifactRoot 同一模式：环境变量覆盖 + 默认相对路径。
func pluginStorageRoot() string {
	if root := strings.TrimSpace(os.Getenv("GO_WP_PLUGIN_ROOT")); root != "" {
		return root
	}
	return filepath.Join("public", "runtime", "plugins")
}

// Install 安装/升级插件：安全解包 → manifest 校验 → L1 迁移 → 存储 → registry 记账。
func (s *Service) Install(ctx context.Context, zipBytes []byte) (res *plugindto.PluginResp, err error) {
	if len(zipBytes) == 0 {
		return nil, errors.New(pluginenums.ErrInstallParse)
	}
	// 1. 解包（内存暂存，全部校验通过才落盘）。
	files, err := extractZipSafe(zipBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrUnsafePackage, err)
	}
	manifestRaw, ok := files["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("%s: 缺少 manifest.json", pluginenums.ErrInstallParse)
	}
	// 2. manifest 白名单校验。
	manifest, err := plugincomp.ParseManifest(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrInstallParse, err)
	}
	// 3. 组件模板存在性校验（声明了模板但包里没有 → 拒绝）。
	for _, c := range manifest.Components {
		key := filepath.ToSlash(filepath.Join("components", c.Template))
		if _, ok := files[key]; !ok {
			return nil, fmt.Errorf("%s: 组件 %s 的模板 %s 不在包内", pluginenums.ErrInstallParse, c.Name, c.Template)
		}
	}
	// 4. 查 registry（迁移版本策略需要现有行判断全新/升级/幂等；nil = 全新安装）。
	now := time.Now().UTC()
	row, gerr := s.m.Get(ctx, manifest.ID)
	if gerr != nil {
		row = nil
	}
	// 5. L1 数据层迁移（manifest 校验通过后、落盘前；失败整体失败且事务回滚，
	//    不留半成品 schema）。
	if err = s.migrateSchema(ctx, manifest, files, row); err != nil {
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrMigrationFailed, err)
	}
	// 6. 落盘存储（{root}/{id}/{version}/）。
	target := filepath.Join(pluginStorageRoot(), manifest.ID, manifest.Version)
	if err = writePluginFiles(target, files); err != nil {
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrStorageFailure, err)
	}
	// 7. registry 记账（新装或升级；升级清理旧版本目录；SchemaVersion = manifest.SchemaVersion）。
	if row == nil {
		row = &pluginmodel.Entity{PluginID: manifest.ID, Enabled: true, InstalledAt: now}
	} else {
		if row.Version != manifest.Version {
			removePluginVersionDir(manifest.ID, row.Version)
		}
		row.Enabled = true // 重新安装视为启用
	}
	row.Name = manifest.Name
	row.Version = manifest.Version
	row.SchemaVersion = manifest.SchemaVersion
	row.Manifest = manifestRaw
	row.StoragePath = target
	row.UpdatedAt = now
	if err = s.m.Update(ctx, row); err != nil {
		// 更新失败时退回插入（例如记录被外部删掉）。原来的 `_ = s.m.Create(...)`
		// 把插入失败也一并吞掉，结果是「注册行根本没写进库」却返回安装成功 ——
		// 插件在列表里时有时无，且没有任何错误可查。
		if cerr := s.m.Create(ctx, row); cerr != nil {
			return nil, fmt.Errorf("插件注册行写入失败: %w", cerr)
		}
	}
	return toResp(row, false), nil
}

// extractZipSafe 安全解包到内存 map（路径 → 内容）。
// zip slip 防护：清路径必须相对且不含上溯；扩展名/大小/条目数白名单。
func extractZipSafe(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("zip 打开失败: %w", err)
	}
	if len(zr.File) > zipMaxEntries {
		return nil, fmt.Errorf("条目数超限（上限 %d）", zipMaxEntries)
	}
	files := make(map[string][]byte, len(zr.File))
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		clean := filepath.ToSlash(filepath.Clean(name))
		if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || clean == ".." {
			return nil, fmt.Errorf("路径穿越拒绝: %q", name)
		}
		if filepath.IsAbs(name) {
			return nil, fmt.Errorf("绝对路径拒绝: %q", name)
		}
		ext := strings.ToLower(filepath.Ext(clean))
		if !extWhitelist[ext] {
			return nil, fmt.Errorf("扩展名 %q 不在白名单: %q", ext, clean)
		}
		if f.UncompressedSize64 > zipMaxFileBytes {
			return nil, fmt.Errorf("文件过大: %q", clean)
		}
		rc, oerr := f.Open()
		if oerr != nil {
			return nil, oerr
		}
		buf, rerr := io.ReadAll(io.LimitReader(rc, zipMaxFileBytes+1))
		_ = rc.Close()
		if rerr != nil {
			return nil, rerr
		}
		if len(buf) > zipMaxFileBytes {
			return nil, fmt.Errorf("文件过大: %q", clean)
		}
		total += uint64(len(buf))
		if total > zipMaxBytes {
			return nil, fmt.Errorf("解包总大小超限（上限 %dMB）", zipMaxBytes>>20)
		}
		files[clean] = buf
	}
	return files, nil
}

// writePluginFiles 落盘（target 目录重建，确保干净）。
func writePluginFiles(target string, files map[string][]byte) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for name, content := range files {
		dst := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// removePluginStorage 删除插件全部存储（卸载）。
func removePluginStorage(pluginID string) {
	_ = os.RemoveAll(filepath.Join(pluginStorageRoot(), pluginID))
}

// removePluginVersionDir 删除指定版本目录（升级清理）。
func removePluginVersionDir(pluginID, version string) {
	if pluginID == "" || version == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(pluginStorageRoot(), pluginID, version))
}

// schemaPrefix L1 数据层 schema 名前缀（docs/06 §8）。
const schemaPrefix = "plugin_"

// schemaNameFor 由插件 ID 派生 schema 名（plugin_<id>）。
func schemaNameFor(pluginID string) string {
	return schemaPrefix + pluginID
}

// dropSchemaSQL 生成 DROP SCHEMA 语句（标识符加双引号，兼容含连字符的 ID；
// 插件 ID 经 plugincomp 白名单校验，不含双引号/反斜杠，拼接无注入面）。
func dropSchemaSQL(pluginID string) string {
	return `DROP SCHEMA IF EXISTS "` + schemaNameFor(pluginID) + `" CASCADE`
}

// migrateSchema 执行插件 L1 数据层迁移，返回 nil 表示成功或无需迁移。
//
// 版本策略（开发阶段从简，AGENTS.md 允许不兼容旧数据、直接重构）：
//   - 空 Migrations（纯展示插件）→ 不执行任何迁移，直接返回 nil；
//   - 全新安装（existing == nil）→ 按文件名字典序执行全部迁移文件；
//   - 升级（existing.SchemaVersion != m.SchemaVersion）→ 采用「重装 =
//     DROP SCHEMA CASCADE 后跑全量」的简单策略，不做「文件名序号 > 旧版本」
//     的增量比对——迁移文件内自带 CREATE SCHEMA IF NOT EXISTS，重跑全量天然幂等；
//   - 同版本（existing.SchemaVersion == m.SchemaVersion）→ 幂等跳过，只更新元数据。
//
// 整体包在一个事务内执行，任一条语句失败即整体回滚，不留半成品 schema。
func (s *Service) migrateSchema(ctx context.Context, m *plugincomp.Manifest, files map[string][]byte, existing *pluginmodel.Entity) (err error) {
	if m.Migrations == "" {
		return nil // 纯展示插件，无自有表
	}
	if existing != nil && existing.SchemaVersion == m.SchemaVersion {
		return nil // 同版本重复安装，幂等跳过
	}
	// 收集 {Migrations}/ 目录下 *.sql 文件，按文件名字典序排序（版本化顺序）。
	names := collectMigrationSQL(m.Migrations, files)
	if len(names) == 0 {
		return fmt.Errorf("迁移目录 %q 下无 .sql 文件", m.Migrations)
	}
	schemaName := schemaNameFor(m.ID)
	// 单事务执行：重装先 DROP 旧 schema，再逐条执行全部迁移。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if existing != nil {
			if err := s.m.ExecTx(ctx, tx, dropSchemaSQL(m.ID)); err != nil {
				return fmt.Errorf("清理旧 schema %s: %w", schemaName, err)
			}
		}
		// 锁定 search_path：迁移里未限定 schema 的对象全部落在插件自己的 schema，
		// 不会外溢到 public（与 validatePluginStatement 的 public. 拒绝互为纵深）。
		if err := s.m.ExecTx(ctx, tx, "SET LOCAL search_path TO "+quoteSchemaIdent(schemaName)); err != nil {
			return fmt.Errorf("锁定 search_path 到 %s 失败: %w", schemaName, err)
		}
		for _, name := range names {
			for _, stmt := range migrations.SplitStatements(string(files[name])) {
				if stmt == "" {
					continue
				}
				if verr := validatePluginStatement(stmt); verr != nil {
					return fmt.Errorf("迁移文件 %s: %w", name, verr)
				}
				if err := s.m.ExecTx(ctx, tx, stmt); err != nil {
					return fmt.Errorf("迁移文件 %s 执行失败: %w", name, err)
				}
			}
		}
		return nil
	})
}

// pluginSQLDeny 插件迁移语句的危险模式黑名单。
//
// 背景（全项目审查发现）：迁移 SQL 直接来自插件 zip，此前逐条 tx.Exec 无任何
// 限制 —— 拥有 plugin:install 权限的低权管理员可借插件包执行任意 SQL（等价提权 DBA，
// 可 INSERT sys_admin / 改 sys_casbin_rule）。插件迁移的正当需求只有「在自己的
// plugin_<id> schema 下建表、建索引、填默认数据」，因此执行器加了两道防线：
//  1. 执行前 SET LOCAL search_path 锁定到插件自己的 schema（未限定对象不外溢）；
//  2. 下列模式一律拒绝（跨库破坏、角色与权限变更、文件与外部访问、系统目录直读）。
var pluginSQLDeny = []*regexp.Regexp{
	regexp.MustCompile(`\bdrop\s+(database|schema|tablespace)\b`),
	regexp.MustCompile(`\b(create|alter|drop)\s+(role|user)\b`),
	regexp.MustCompile(`\bgrant\b|\brevoke\b`),
	regexp.MustCompile(`\balter\s+system\b`),
	regexp.MustCompile(`\bset\s+role\b`),
	regexp.MustCompile(`\bsecurity\s+definer\b`),
	regexp.MustCompile(`\bcopy\b`),
	regexp.MustCompile(`\bpg_read_file\b|\bpg_write_file\b|\bpg_ls_dir\b|\bpg_read_binary_file\b`),
	regexp.MustCompile(`\blo_import\b|\blo_export\b`),
	regexp.MustCompile(`\bpg_authid\b|\bpg_shadow\b|\bpg_catalog\b|\binformation_schema\b`),
	regexp.MustCompile(`\bpublic\s*\.`),
	regexp.MustCompile(`\bdblink\b|\bpostgres_fdw\b`),
}

// sqlBlockCommentRe 块注释（校验前剥离，避免注释文本触发误判）。
var sqlBlockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

// stripSQLComments 剥离行注释与块注释，只对真正的语句文本做关键字匹配。
func stripSQLComments(stmt string) string {
	s := sqlBlockCommentRe.ReplaceAllString(stmt, " ")
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// validatePluginStatement 校验单条插件迁移语句，命中黑名单即拒绝执行。
func validatePluginStatement(stmt string) error {
	lower := strings.ToLower(stripSQLComments(stmt))
	for _, re := range pluginSQLDeny {
		if re.MatchString(lower) {
			return fmt.Errorf("迁移语句被安全策略拒绝（命中 %s）：插件迁移只能操作自己的 schema", re.String())
		}
	}
	return nil
}

// quoteSchemaIdent 双引号包裹 schema 标识符（ID 经 plugincomp 白名单校验，无注入面）。
func quoteSchemaIdent(name string) string {
	return `"` + name + `"`
}

// collectMigrationSQL 收集迁移目录下 *.sql 文件的键，按字典序排序返回。
func collectMigrationSQL(dir string, files map[string][]byte) []string {
	prefix := dir + "/"
	var names []string
	for key := range files {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ".sql") {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names
}

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
	// 组件库默认提示（manifest 未声明 hint 时）：按默认语言取一次，循环内所有组件共用。
	defaultHint := defaultComponentHint()
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
				Hint:  orDefault(c.Hint, defaultHint),
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

// defaultComponentHint 插件组件在组件库里的默认提示（manifest 未声明 hint 时，docs/06 §5）。
//
// 取词用**默认语言**：本函数的产物按启用集指纹进程内缓存、跨请求共享（见文件头），
// 没有请求语言可用 —— 与构建期组件文案的既有口径一致（i18n.TranslateFunc(lang)，
// lang 来自构建配置）。词条缺失回落中文兜底，绝不把裸 key 写进组件库。
//
// 为什么不能继续留裸中文：这条提示随 Assembly.Components 进工作台组件库的 JSON
// （workbench_handle.go 的 palette 注入）并渲染给运营，裸中文在英文站点上无从替换；
// 登记成 key 之后可被词条覆盖，取词链与后台页面同一套（pkg/i18n）。
func defaultComponentHint() string {
	return i18n.TranslateFunc(i18n.GetDefaultLang())(pluginenums.HintComponentDefault, "插件组件")
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
//   - update_time：启停与升级都会更新该行；
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
