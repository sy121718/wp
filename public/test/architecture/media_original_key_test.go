package architecture

// media_original_key_test.go — 媒体原图存储 key 的「形态封闭」门禁。
//
// 判据：生产代码（internal/ 与 pkg/ 下的非 _test.go 文件）不得出现 `PreserveName: true`。
//
// 为什么需要它：/storage 的缓存头策略（internal/middleware/builtin/storage_cache.go）
// 按「URL 是否带指纹」二分 —— 带指纹的变体发 immutable，其余（含原图）发 must-revalidate。
// 变体侧的判据是**文件名里有没有完整指纹形状**，而这条判据的正确性不由正则本身担保，
// 它依赖一个外部前提：**原图 key 恒为 <id>.<ext>**（两个 upload.Upload 调用点都显式传
// ObjectKey），永不采用用户原名。前提成立时，原图 URL 不可能长得像带指纹的变体名，
// immutable 判定不会误命中 —— 于是「放宽正则」与「收紧正则」在生产上等价安全。
//
// 前提一旦失效，后果静默且严重：新增一个 PreserveName: true 的上传入口后，
// 用户上传一个名为 x_thumb-1-ab12cd34.jpg 的文件就会被判为不可变 —— 而换图是原地覆盖
// 字节（media_replace.go 保持文件名不变），老访客将在 max-age 内一直看到旧图，
// **且没有任何测试或门禁会报警**。这条门禁就是为那个未来准备的。
//
// 现状（2026-10-02 实测）：生产代码零调用点；唯一使用在
// public/test/pkg/upload/upload_functional_test.go（测试构造，不受本门禁约束）。
// 生产数据佐证：sys_attachment 419 行中纯数字 id = 413 行、时间戳_hex 随机名 = 6 行、
// 采用用户原名的 = 0 行。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/public/test/support"
)

// TestNoPreserveNameInProduction 生产代码不得让存储 key 采用用户原名。
func TestNoPreserveNameInProduction(t *testing.T) {
	root := support.RepoRoot(t)
	var offenders []string

	for _, dir := range []string{filepath.Join(root, "internal"), filepath.Join(root, "pkg")} {
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if perr != nil {
				// 解析失败交给编译与其它门禁去报，本测试不越界判定。
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "PreserveName" {
					return true
				}
				// 只拦「显式置真」：零值与显式 false 都不改变 key 形态。
				if v, ok := kv.Value.(*ast.Ident); ok && v.Name == "true" {
					if rel, rerr := filepath.Rel(root, path); rerr == nil {
						offenders = append(offenders, rel)
					}
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("扫描 %s 失败: %v", dir, walkErr)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("生产代码出现 PreserveName: true —— 存储 key 会采用用户原名，"+
			"/storage 的 immutable 判定随之不再安全（原图 URL 可能长得像带指纹的变体名，"+
			"换图是原地覆盖字节，老访客会长期看到旧图）。\n"+
			"若确实需要保留原名，必须同时改 internal/middleware/builtin/storage_cache.go "+
			"的判据（改成按记录判定而不是按文件名），不能只放开这个开关。\n命中文件: %v", offenders)
	}
}
