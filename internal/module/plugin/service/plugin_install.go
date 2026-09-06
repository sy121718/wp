package pluginservice

// plugin_install.go — 插件 zip 的安全安装（docs/06-plugin-system.md §11 安全边界）。
//
// 未信任输入防线：
//   - zip slip 路径穿越防护（清理后必须仍在目标目录内，拒绝绝对路径/上溯）；
//   - 扩展名白名单（模板/schema/资产，拒绝可执行与未知类型）；
//   - 解包尺寸与条目数上限；
//   - manifest 经 plugincomp.ParseManifest 白名单校验（属性名/选择器/值/控件类型）；
//   - 存储路径由服务端拼接（pluginID/version 来自校验后的 manifest，白名单字符）。

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_wp/internal/builder/plugincomp"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	pluginmodel "go_wp/internal/module/plugin/model"
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

// pluginStorageRoot 插件解包根目录（public/runtime 为运行时产物区，
// 未挂载任何对外静态路由：/storage 只挂 public/storage，/site 只挂激活产物）。
var pluginStorageRoot = filepath.Join("public", "runtime", "plugins")

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
	target := filepath.Join(pluginStorageRoot, manifest.ID, manifest.Version)
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
		_ = s.m.Create(ctx, row)
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
	_ = os.RemoveAll(filepath.Join(pluginStorageRoot, pluginID))
}

// removePluginVersionDir 删除指定版本目录（升级清理）。
func removePluginVersionDir(pluginID, version string) {
	if pluginID == "" || version == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(pluginStorageRoot, pluginID, version))
}
