package ui

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"
)

//go:embed all:assets
var assetsFS embed.FS

// AssetsFS 返回公共 UI 资产源，逻辑键与 /static URL 一致（js/ui/...、css/...）。
//
// 应用装配时把它与业务侧资产合并进同一复合 FS：框架只拥有被公共模板引用的那一份，
// 业务专用脚本仍由应用提供，二者不同名。
func AssetsFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic("pkg/view/ui: 公共资产 embed 子目录失败: " + err.Error())
	}
	return sub
}

// AssetFile 按逻辑键读一个公共资产；不存在时返回可定位的错误。
func AssetFile(name string) ([]byte, error) {
	b, err := fs.ReadFile(AssetsFS(), name)
	if err != nil {
		return nil, fmt.Errorf("读取公共 UI 资产 %s: %w", name, err)
	}
	return b, nil
}

// Layer 是复合资产 FS 的一层：名称只用于报错定位。
type Layer struct {
	Name string
	FS   fs.FS
}

// CompositeFS 按层叠加资产源，逻辑键重名在构造时显式报错。
//
// 不提供逐层 fallback 搜索：同一逻辑键只能有一个属主，否则「框架资产被应用副本
// 静默覆盖」这类问题只会在浏览器里表现为行为不一致。
type CompositeFS struct {
	layers []Layer
	owner  map[string]string
}

// NewCompositeFS 校验各层无重名后返回复合 FS。
func NewCompositeFS(layers ...Layer) (*CompositeFS, error) {
	c := &CompositeFS{owner: map[string]string{}}
	for _, l := range layers {
		if l.FS == nil {
			return nil, fmt.Errorf("资产层 %s 为空", l.Name)
		}
		err := fs.WalkDir(l.FS, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			key := normalizeAssetKey(p)
			if prev, dup := c.owner[key]; dup {
				return fmt.Errorf("公共资产逻辑键冲突：%s 同时属于 %s 与 %s", key, prev, l.Name)
			}
			c.owner[key] = l.Name
			return nil
		})
		if err != nil {
			return nil, err
		}
		c.layers = append(c.layers, l)
	}
	if len(c.layers) == 0 {
		return nil, errors.New("资产复合 FS 至少需要一层")
	}
	return c, nil
}

// Stat 返回逻辑键的元数据；根目录固定存在。
func (c *CompositeFS) Stat(name string) (fs.FileInfo, error) {
	key := normalizeAssetKey(name)
	if key == "" {
		return dirInfo{}, nil
	}
	if _, ok := c.owner[key]; !ok {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	for _, l := range c.layers {
		if info, err := fs.Stat(l.FS, key); err == nil {
			return info, nil
		}
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// Open 按层顺序找到第一个持有该逻辑键的资产源。
func (c *CompositeFS) Open(name string) (fs.File, error) {
	key := normalizeAssetKey(name)
	if key == "" {
		return &dirFile{fs: c}, nil
	}
	if _, ok := c.owner[key]; !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	for _, l := range c.layers {
		if f, err := l.FS.Open(key); err == nil {
			return f, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadDir 合并各层同一目录的条目（按名称去重），供 fs.WalkDir 使用。
func (c *CompositeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	dir := normalizeAssetKey(name)
	if dir == "" {
		dir = "."
	}
	seen := map[string]fs.DirEntry{}
	for _, l := range c.layers {
		entries, err := fs.ReadDir(l.FS, dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			seen[e.Name()] = e
		}
	}
	if len(seen) == 0 {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	out := make([]fs.DirEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Keys 返回全部逻辑键（排序后可确定性对比开发与生产）。
func (c *CompositeFS) Keys() []string {
	out := make([]string, 0, len(c.owner))
	for k := range c.owner {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func normalizeAssetKey(name string) string {
	for len(name) > 1 && name[0] == '/' {
		name = name[1:]
	}
	if name == "." {
		return ""
	}
	return name
}

// dirInfo 是复合 FS 根目录的元数据。
type dirInfo struct{}

func (dirInfo) Name() string       { return "." }
func (dirInfo) Size() int64        { return 0 }
func (dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (dirInfo) ModTime() time.Time { return time.Time{} }
func (dirInfo) IsDir() bool        { return true }
func (dirInfo) Sys() any           { return nil }

// dirFile 是复合 FS 根目录的只读句柄。
type dirFile struct {
	fs   *CompositeFS
	done bool
}

func (f *dirFile) Stat() (fs.FileInfo, error) { return dirInfo{}, nil }
func (f *dirFile) Close() error               { return nil }
func (f *dirFile) Read([]byte) (int, error)   { return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid} }
func (f *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.done {
		return nil, nil
	}
	f.done = true
	return f.fs.ReadDir(".")
}
