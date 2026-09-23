package contenthttp

// article_new_page.go — 文章新建整页（对齐商品 /admin/products/new 的整页形态，弃抽屉）：
// 左栏标题 / 路径 / 正文（Trix 富文本，partials/rich_editor.html），右栏实时预览
// + 「可视化编辑」入口（未保存时置灰，提示先保存）。表单 POST 复用既有
// /admin/articles/create，字段名 title / slug / body 与原抽屉逐字一致，成功后
// 照旧 302 进编辑页 —— 路由注册处（article_router.go）对本页挂
// CasbinMiddlewareForPath("/api/content/create")：能建文章的人才能打开新建页。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
)

// ArticleNewPage GET /admin/articles/new：文章新建整页。
//
// 新建页不取数（标题 / 路径全空、正文空），只渲染表单；预览由前端 live-preview.js
// 从 Trix 编辑器同步，服务端零往返。Form 键集与编辑页 articleEditPageData 的 form
// 对齐（模板点号取值，缺键会让 Jet 报错并截断整页）。
func (h *articlePageHandle) ArticleNewPage(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/content/article_new.html", shell.Prepare(c, gin.H{
		"title": articleNewTitle,
		"menu":  "articles",
		"Form": gin.H{
			"ID": "", "Slug": "", "Title": "", "Body": "",
			"Revision": int64(0), "UpdatedAt": "",
		},
	}))
}
