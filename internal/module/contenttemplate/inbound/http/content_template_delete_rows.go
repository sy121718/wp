package contenttemplatehttp

import (
	"github.com/gin-gonic/gin"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
)

// contentTemplateDeleteRow 是交 Jet 的逐条事实：结果与错误 key/兜底，不含成品句。
type contentTemplateDeleteRow struct {
	ID       string
	Deleted  bool
	FactKey  string
	Fallback string
}

func contentTemplateDeleteRows(c *gin.Context, result contenttemplatedto.BulkDeleteResp, errs []error) []contentTemplateDeleteRow {
	rows := make([]contentTemplateDeleteRow, 0, len(result.Items))
	for i, item := range result.Items {
		row := contentTemplateDeleteRow{ID: item.ID, Deleted: item.Deleted}
		if i < len(errs) && errs[i] != nil {
			msg := contentTemplateErrMsg(c, errs[i])
			row.FactKey, row.Fallback = msg.Key, msg.Fallback
		}
		rows = append(rows, row)
	}
	return rows
}
