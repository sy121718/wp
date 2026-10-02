package builtin

// static_fs.go — 禁止目录列表、同时保住内核零拷贝的 http.FileSystem。
//
// 【为什么不能用 gin.Dir(root, false)】
// 它的 OnlyFilesFS.Open 对**所有**条目无条件返回 neutralizedReaddirFile{f} ——
// 一个**值类型**、且嵌入的是 http.File **接口**。接口的方法集只有
// Close/Read/Seek/Readdir/Stat，不含 WriteTo，于是 *os.File 的 WriteTo 被抹掉，
// sendfile 的第二半条件（见 sendfile.go）直接不成立。而它对文件的这层包装是纯
// 副作用：需要禁的是**目录列表**，而 http.FileServer 只在「目录且没有 index.html」
// 时才走 dirList。
//
// 【禁用目录列表的正确做法不是让 Readdir 返回空】
// Readdir 返回 (nil, nil) 不是错误，dirList 会把它渲染成 **200 + 空 <pre>** ——
// 「目录不可浏览」被实现成了「一个 200 的空页面」。gin 用「fs 是 *OnlyFilesFS 就
// 预设 404」来兜住这个 200，而那条兜底绑在 FS 的**动态类型**上，我们换掉类型就
// 失效了。
//
// 所以这里在 Open 层解决：目录只有在**确实存在 index.html** 时才被承认存在；
// 否则返回 os.ErrNotExist，http.FileServer 会走它自己的 toHTTPError → 404。
// 可观测行为与替换前逐条一致，且不再依赖任何类型断言。

import (
	"net/http"
	"os"
	"path"
	"strings"
)

// NoDirListFS 返回禁止目录列表、但对普通文件保留 *os.File 的 http.FileSystem。
func NoDirListFS(root string) http.FileSystem {
	return noDirListFS{inner: http.Dir(root)}
}

type noDirListFS struct{ inner http.FileSystem }

func (fs noDirListFS) Open(name string) (http.File, error) {
	f, err := fs.inner.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		// Stat 失败后这个 fd 不会再交给任何人，必须在这里释放，否则每次探测都漏一个 fd。
		_ = f.Close()
		return nil, err
	}
	if !st.IsDir() {
		// 普通文件：原样透出，保住 *os.File 的 WriteTo（sendfile 条件②）。
		return f, nil
	}

	// 目录：只有真的能拿出一份 index.html 才承认它存在。
	// 这里只做存在性探测；真正的内容仍由上层（http.FileServer 的 index 分支）
	// 通过一次正常 Open 取得，所以零拷贝的属性由那一次的返回值决定。
	probe, err := fs.inner.Open(path.Join(strings.TrimSuffix(name, "/"), "index.html"))
	if err != nil {
		_ = f.Close()
		return nil, os.ErrNotExist
	}
	_ = probe.Close()
	return noDirListDir{f}, nil
}

// noDirListDir 是纵深防御：万一有调用方绕过上面的 index.html 判定、直接把目录
// 交给 http.FileServer，dirList 读到的也永远是空，不会泄漏条目名。
//
// 注意它**不是**目录不可浏览的实现手段 —— 那件事由上面的 os.ErrNotExist 负责。
// 单靠它会得到 200 + 空页，而正确结果是 404。
type noDirListDir struct{ http.File }

func (d noDirListDir) Readdir(int) ([]os.FileInfo, error) { return nil, nil }
