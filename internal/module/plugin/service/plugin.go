package pluginservice

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"go_wp/internal/module/plugin/contract"
	"go_wp/internal/module/plugin/dto"
	"go_wp/internal/module/plugin/enums"
	"go_wp/internal/module/plugin/model"
	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/builder/plugincomp"
	"go_wp/pkg/logger"
	"go_wp/public/migrations"
)

const (
	zipMaxBytes     = 50 << 20
	zipMaxEntries   = 500
	zipMaxFileBytes = 10 << 20
)

var extWhitelist = map[string]bool{
	".jet": true, ".json": true, ".css": true, ".js": true, ".svg": true,
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
	".woff2": true, ".woff": true, ".ttf": true, ".sql": true, ".md": true, ".txt": true,
}

func pluginStorageRoot() string {
	if root := strings.TrimSpace(os.Getenv("GO_WP_PLUGIN_ROOT")); root != "" {
		return root
	}
	return filepath.Join("public", "runtime", "plugins")
}

func (s *Service) Install(ctx context.Context, zipBytes []byte) (res *plugindto.PluginResp, err error) {
	if len(zipBytes) == 0 {
		return nil, pluginenums.Err(pluginenums.ErrInstallParse)
	}

	files, err := extractZipSafe(zipBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrUnsafePackage, err)
	}
	manifestRaw, ok := files["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("%s: 缺少 manifest.json", pluginenums.ErrInstallParse)
	}

	manifest, err := plugincomp.ParseManifest(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrInstallParse, err)
	}

	for _, c := range manifest.Components {
		key := filepath.ToSlash(filepath.Join("components", c.Template))
		if _, ok := files[key]; !ok {
			return nil, fmt.Errorf("%s: 组件 %s 的模板 %s 不在包内", pluginenums.ErrInstallParse, c.Name, c.Template)
		}
	}

	now := time.Now().UTC()
	row, gerr := s.m.Get(ctx, manifest.ID)
	if gerr != nil {
		row = nil
	}

	// 安装跨 schema 与文件系统，无法用一个数据库事务整体回滚；
	// 每步失败都补偿已完成的副作用，保证「失败后回到装之前」。
	if err = s.migrateSchema(ctx, manifest, files, row); err != nil {
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrMigrationFailed, err)
	}

	target := filepath.Join(pluginStorageRoot(), manifest.ID, manifest.Version)
	if err = writePluginFiles(target, files); err != nil {
		// 文件没写成：把刚建的 schema 撤掉，不留半套。
		s.rollbackSchema(ctx, manifest.ID)
		return nil, fmt.Errorf("%s: %w", pluginenums.ErrStorageFailure, err)
	}

	if row == nil {
		row = &pluginmodel.Entity{PluginID: manifest.ID, Enabled: true, InstalledAt: now}
	} else {
		if row.Version != manifest.Version {
			removePluginVersionDir(manifest.ID, row.Version)
		}
		row.Enabled = true
	}
	row.Name = manifest.Name
	row.Version = manifest.Version
	row.SchemaVersion = manifest.SchemaVersion
	row.Manifest = manifestRaw
	row.StoragePath = target
	row.UpdatedAt = now
	if err = s.m.Update(ctx, row); err != nil {

		if cerr := s.m.Create(ctx, row); cerr != nil {
			// 注册行没写成：schema 与文件都要撤掉，否则下次安装会撞上残留。
			s.rollbackSchema(ctx, manifest.ID)
			removePluginVersionDir(manifest.ID, manifest.Version)
			return nil, fmt.Errorf("插件注册行写入失败: %w", cerr)
		}
	}
	return toResp(row, false), nil
}

// rollbackSchema 是安装失败时的补偿：撤掉本次可能建出来的 schema。
//
// 它只记日志、不向外抛错：调用方已经在处理安装失败，补偿再失败时
// 能做的只有留下可定位日志供人工清理，不能把原始失败原因盖掉。
func (s *Service) rollbackSchema(ctx context.Context, pluginID string) {
	if strings.TrimSpace(pluginID) == "" {
		return
	}
	if err := s.m.Exec(ctx, dropSchemaSQL(pluginID)); err != nil {
		logger.Scene("plugin").With("plugin_id", pluginID).
			Error(err, "安装失败后回滚插件 schema 失败（需人工清理残留 schema）")
	}
}

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

func removePluginStorage(pluginID string) {
	_ = os.RemoveAll(filepath.Join(pluginStorageRoot(), pluginID))
}

func removePluginVersionDir(pluginID, version string) {
	if pluginID == "" || version == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(pluginStorageRoot(), pluginID, version))
}

const schemaPrefix = "plugin_"

func schemaNameFor(pluginID string) string {
	return schemaPrefix + pluginID
}

func dropSchemaSQL(pluginID string) string {
	return `DROP SCHEMA IF EXISTS "` + schemaNameFor(pluginID) + `" CASCADE`
}

func (s *Service) migrateSchema(ctx context.Context, m *plugincomp.Manifest, files map[string][]byte, existing *pluginmodel.Entity) (err error) {
	if m.Migrations == "" {
		return nil
	}
	if existing != nil && existing.SchemaVersion == m.SchemaVersion {
		return nil
	}

	names := collectMigrationSQL(m.Migrations, files)
	if len(names) == 0 {
		return fmt.Errorf("迁移目录 %q 下无 .sql 文件", m.Migrations)
	}
	schemaName := schemaNameFor(m.ID)

	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if existing != nil {
			if err := s.m.ExecTx(ctx, tx, dropSchemaSQL(m.ID)); err != nil {
				return fmt.Errorf("清理旧 schema %s: %w", schemaName, err)
			}
		}

		if err := s.setLocalPluginRole(tx); err != nil {
			return err
		}

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

var PluginMigrationRole = "go_wp_app"

func (s *Service) setLocalPluginRole(tx *gorm.DB) error {
	role := strings.TrimSpace(PluginMigrationRole)
	if role == "" {
		return fmt.Errorf("插件迁移角色未配置：拒绝在超级用户身份下执行外部 SQL")
	}
	if err := s.m.ExecTx(context.Background(), tx, "SET LOCAL ROLE "+quoteSchemaIdent(role)); err != nil {
		return fmt.Errorf("切换到插件迁移角色 %s 失败（请先跑 scripts/rls-role-setup.sh）：%w", role, err)
	}
	return nil
}

var sqlBlockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

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

func validatePluginStatement(stmt string) error {
	lower := strings.ToLower(stripSQLComments(stmt))
	for _, re := range pluginSQLDeny {
		if re.MatchString(lower) {
			return fmt.Errorf("迁移语句被安全策略拒绝（命中 %s）：插件迁移只能操作自己的 schema", re.String())
		}
	}
	return nil
}

func quoteSchemaIdent(name string) string {
	return `"` + name + `"`
}

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

	// service 不取词：空提示保持为空，成品文案由消费方按自己的语言决定。

	for _, row := range rows {

		if st, serr := os.Stat(row.StoragePath); serr != nil || !st.IsDir() {
			continue
		}
		manifest, perr := plugincomp.ParseManifest(row.Manifest)
		if perr != nil {
			continue
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
				Hint:  c.Hint,
				Props: defaultProps(c.Props),
			})
		}

		for _, p := range manifest.Presets {
			asm.Presets = append(asm.Presets, plugindto.PresetSummary{
				ID:        p.ID,
				Label:     p.Label,
				Category:  p.Category,
				Thumbnail: p.Thumbnail,
				Document:  p.Document,
			})
		}

		if css := pluginExtraCSS(manifest.ID, row.StoragePath); css != "" {
			asm.ExtraCSS = append(asm.ExtraCSS, css)
		}
	}

	slices.SortFunc(asm.Components, func(a, b plugindto.ComponentSummary) int {
		return strings.Compare(a.Type, b.Type)
	})
	return asm, nil
}

func defaultProps(props map[string]plugincomp.PropControl) map[string]any {
	out := make(map[string]any, len(props))
	for k, ctl := range props {
		if ctl.Default != nil {
			out[k] = ctl.Default
		}
	}
	return out
}

var cssImportRe = regexp.MustCompile(`(?i)@import[^;]*;?`)

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
			continue
		}
		css := strings.ReplaceAll(string(data), "</style", "")

		css = cssImportRe.ReplaceAllString(css, "")
		sb.WriteString("/* plugin:" + pluginID + ":" + name + " */\n")
		sb.WriteString(css)
		sb.WriteString("\n")
	}
	return sb.String()
}

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

func (s *Service) cachedAssembly(fp string) *plugincontract.Assembly {
	s.asmMu.Lock()
	defer s.asmMu.Unlock()
	if s.asmFingerprint == fp && s.asmCache != nil {
		return s.asmCache
	}
	return nil
}

func (s *Service) storeAssembly(fp string, asm *plugincontract.Assembly) {
	s.asmMu.Lock()
	defer s.asmMu.Unlock()
	s.asmFingerprint, s.asmCache = fp, asm
}
