package builtin

// SiteRedirectMiddleware 让静态访问面认识「重定向产物」（改 URL 后的旧路径 301）。
//
// 为什么需要它：改 URL 时旧路径会落一份 redirect.json 并激活（pipeline.NewRedirectArtifact
// + LocalPublicationStore.Activate），但访问面是 http.FileServer —— 它只读文件、
// 不认识 redirect.json。缺这个中间件时，勾了「保留旧链接」的旧路径表现是 **404**：
// 用户在界面上选择了一个系统并不执行的承诺。
//
// 介入代价：非重定向请求多一次 readlink（微秒级）；命中重定向才额外读 redirect.json。
// 判定只看「符号链接指向的目录里有没有 redirect.json」，不查库、不读路由表 ——
// 访问面「零查库零模板」的不变量不被破坏。
//
// 与 StaticGzipMiddleware 同组挂载，顺序在先：301 响应没有 body，压缩无从谈起。

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
	"go_wp/internal/seo"
)

// siteFacePrefix 访问面挂载前缀（与 setupStaticFace 的 Group("/site") 保持一致）。
const siteFacePrefix = "/site"

// SiteRedirectMiddleware 见文件头注释。
func SiteRedirectMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if target, ok := indexAliasRedirectOf(c.Request.URL.Path); ok {
			c.Redirect(http.StatusMovedPermanently, target)
			c.Abort()
			return
		}
		target, code, ok := redirectTargetOf(c.Request.URL.Path)
		if !ok {
			c.Next()
			return
		}
		c.Redirect(code, target)
		c.Abort()
	}
}

// indexAliasRedirectOf /index 语言根别名 301 到规范路径（I18N-022）。
func indexAliasRedirectOf(fullPath string) (target string, ok bool) {
	if !strings.HasPrefix(fullPath, siteFacePrefix+"/") {
		return "", false
	}
	rel := strings.TrimPrefix(fullPath, siteFacePrefix)
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" {
		return "", false
	}
	sitePath := rel
	if !strings.HasPrefix(sitePath, "/") {
		sitePath = "/" + sitePath
	}
	if !seo.IsPublicIndexAlias(sitePath) {
		return "", false
	}
	return seo.CanonicalPublicPath(sitePath), true
}

// redirectTargetOf 判断访问面路径是否指向重定向产物，返回目标路径与状态码。
//
// 三条否定条件都返回 ok=false 交给静态面处理，中间件不改变任何其他请求的行为：
// 不在访问面前缀内、路径越界、链接目标不是重定向产物。
func redirectTargetOf(fullPath string) (target string, code int, ok bool) {
	if !strings.HasPrefix(fullPath, siteFacePrefix+"/") {
		return "", 0, false
	}
	// 先连前缀斜杠一起去掉再 Clean：filepath.Clean("/old-path") 仍是绝对路径
	//（前导斜杠保留），直接 Join 会忽略激活根目录、指向文件系统根。
	// 带尾斜杠的目录访问形式在这里一并归一（Clean 会去掉尾斜杠）。
	//
	// clean 而不是原样拼接：rel 可能带 ".."（/site/../../etc/passwd），
	// 直接 Join 会逃出激活目录。静态面（http.Dir）自己有同类防护，但本中间件
	// 要自己读盘，必须自己挡。绝对路径、"." 与 ".." 前缀一并拒绝。
	rel := filepath.Clean(strings.TrimPrefix(fullPath, siteFacePrefix+"/"))
	if rel == "." || rel == "" || filepath.IsAbs(rel) ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", 0, false
	}
	link := filepath.Join(pipeline.ActiveRoot(), rel)
	// 只看符号链接：真目录与普通文件都不是激活产物（激活一律是 symlink）。
	linkTarget, err := os.Readlink(link)
	if err != nil {
		return "", 0, false
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(link), linkTarget))
	data, err := os.ReadFile(filepath.Join(dir, "redirect.json"))
	if err != nil {
		return "", 0, false
	}
	directive, perr := pipeline.ParseRedirectEntry(data)
	if perr != nil || directive == nil || strings.TrimSpace(directive.TargetPath) == "" {
		return "", 0, false
	}
	code = directive.StatusCode
	if code != http.StatusMovedPermanently && code != http.StatusFound {
		// 产物只可能带 301/302（NewRedirectArtifact 卡住），兜底按永久处理。
		code = http.StatusMovedPermanently
	}
	return directive.TargetPath, code, true
}
