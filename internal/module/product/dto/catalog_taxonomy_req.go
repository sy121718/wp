package productdto

import (
	"net/url"
	"strings"
)

// CatalogTaxonomyReq 是分类、品牌、标签、属性的目录查询条件，不包含页面或表单状态。
type CatalogTaxonomyReq struct {
	ProjectID string `form:"project"`
	Keyword   string `form:"keyword"`
	Variation string `form:"variation"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

func (q *CatalogTaxonomyReq) BindQuery(values url.Values) error {
	q.ProjectID = strings.TrimSpace(q.ProjectID)
	if q.ProjectID == "" {
		q.ProjectID = strings.TrimSpace(values.Get("projectId"))
	}
	q.Keyword = strings.TrimSpace(q.Keyword)
	q.Variation = strings.TrimSpace(q.Variation)
	if q.Variation != "1" && q.Variation != "0" {
		q.Variation = ""
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 {
		q.Size = 20
	}
	if q.Size > 100 {
		q.Size = 100
	}
	return nil
}
