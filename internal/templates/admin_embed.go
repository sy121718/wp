// admin_embed.go — 后台模板与静态资源的 embed 出口（审计 OSS-018）。
//
// 背景：生产构建此前仍从磁盘读 internal/templates/（后台模板与 /static 都是），
// 于是「go build 出来的那个二进制」必须与源码树一起交付 —— 丢掉了 Go 项目单文件
// 分发的优势，对自托管用户尤其不友好。组件模板、片段模板早已是 embed
// （components_embed.go / fragment_render.go），这里补上最后两块。
//
// 分法（调用方按运行模式选择）：
//   - debug / test：照旧读磁盘。改模板与 CSS 立即生效是开发期最需要的反馈，
//     embed 会把「改一行看效果」变成「重新编译再看」。
//   - release：从 embed.FS 读，二进制自带全部模板与静态资源。
package templates

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
)

// adminTemplatesFS 后台页面模板（根为 templates 包目录下的 admin/）。
//
// 一律用 all: 前缀而不是裸目录名：go:embed 默认忽略以 _ 与 . 开头的文件，
// 而 static/js/ui/ 下就有 _util.js —— 漏掉它不会报错，只会在生产模式下多一个 404。
// 这类「打进去了但少一个」的静默差异，正是单二进制分发最容易踩的地方。
//
//go:embed all:admin
var adminTemplatesFS embed.FS

// staticAssetsFS static 目录的全部资产（JS/CSS/vendor/字体/图片）。
//
// 此前只 embed 了 js/*.js、js/ui/*.js 与 css/ui.css 三项（构建期内联进产物用），
// 运行时 /static 仍读磁盘。这里 embed 整个目录，让「二进制自足」成立。
//
//go:embed all:static
var staticAssetsFS embed.FS

// newAdminTemplateLoader 后台模板的 embed loader。
//
// 注意**不做 fs.Sub**：embed.FS 的根就是本包目录，模板名因此是 admin/login.html ——
// 与磁盘 loader（根为 internal/templates）解析出的名字逐字一致。
// 一度 Sub 到 admin/ 之下，模板名就变成 login.html，生产模式下找不到模板：
// 页面渲染成空体而状态码仍是 200。这类不一致只在切换来源时才暴露，所以两条路径
// 的「根」必须对齐。
func newAdminTemplateLoader() (*embedLoader, error) {
	files, err := listEmbeddedFiles(adminTemplatesFS)
	if err != nil {
		return nil, err
	}
	return newEmbedLoader(adminTemplatesFS, files), nil
}

// EmbeddedStaticFS 返回 /static 的 embed 只读文件系统（禁目录列表）。
//
// 禁列表的语义与 gin.Dir(dir, false) 一致：没有 index 文件时返回错误，
// 而不是把目录内容列给访客 —— 这条是审计 Low 修过的，换成 embed 不能丢。
func EmbeddedStaticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(staticAssetsFS, "static")
	if err != nil {
		return nil, err
	}
	return noDirListFS{inner: http.FS(sub)}, nil
}

// listEmbeddedFiles 递归列出 fs 下的全部文件（embed loader 的 Exists 清单）。
func listEmbeddedFiles(fsys fs.FS) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

// noDirListFS 包装 http.FileSystem 并禁掉目录列表（与 gin.Dir(dir, false) 同语义）。
type noDirListFS struct{ inner http.FileSystem }

func (f noDirListFS) Open(name string) (http.File, error) {
	file, err := f.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return noDirListFile{File: file}, nil
}

type noDirListFile struct{ http.File }

func (f noDirListFile) Readdir(int) ([]fs.FileInfo, error) {
	return nil, errors.New("目录列表已禁用")
}
