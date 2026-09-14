// seo_score_handle.go — 工作台 SEO 评分接口（只读分析，不写产物）。
package dashboardhttp

import (
	"encoding/json"
	"net/http"

	seoscore "go_wp/internal/seo"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// SEOScore 计算草稿文档的 SEO 评分。
// POST /workbench/seo-score（form: draftDocument, url）→ 评分结果 JSON。
func (h *Handle) SEOScore(c *gin.Context) {
	document := json.RawMessage(c.PostForm("draftDocument"))
	if len(document) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, "草稿文档为空")
		return
	}
	res, err := seoscore.ScoreDocument(document, c.PostForm("url"), requestScoreLang(c))
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, "SEO 评分失败: "+err.Error())
		return
	}
	response.Success(c, res)
}
