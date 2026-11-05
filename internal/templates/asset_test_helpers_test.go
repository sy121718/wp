package templates

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// materializeAssets 把复合静态资产写成临时目录（保留逻辑键）。
//
// Node 侧契约测试需要真实文件路径；写出后它们与 /static 读的是同一份
// 复合 FS（pkg 公共 + 应用专用），不再依赖源码头目录布局。
func materializeAssets(t *testing.T) string {
	t.Helper()
	assets, err := StaticAssets()
	if err != nil {
		t.Fatalf("构建静态资产复合 FS 失败：%v", err)
	}
	root := t.TempDir()
	for _, key := range assets.Keys() {
		b, err := fs.ReadFile(assets, key)
		if err != nil {
			t.Fatalf("读取资产 %s 失败：%v", key, err)
		}
		dst := filepath.Join(root, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("建目录失败：%v", err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatalf("写资产 %s 失败：%v", key, err)
		}
	}
	return root
}
