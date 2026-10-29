package productdto

import (
	"strings"

	"github.com/google/uuid"
	productenums "go_wp/internal/module/product/enums"
)

// PublicVariantsReq 公开商品事实查询；不接受构建文档、展示参数或成本字段。
type PublicVariantsReq struct {
	ProjectID  string `form:"projectId" binding:"required,uuid"`
	VariantIDs string `form:"variantIds" binding:"required,max=3699"`
}

func (r *PublicVariantsReq) IDs() ([]string, error) {
	if r == nil || uuid.Validate(r.ProjectID) != nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	parts := strings.Split(r.VariantIDs, ",")
	if len(parts) == 0 || len(parts) > 100 {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, seen := make([]string, 0, len(parts)), map[string]bool{}
	for _, raw := range parts {
		id := strings.TrimSpace(raw)
		if uuid.Validate(id) != nil {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
		if !seen[id] {
			ids, seen[id] = append(ids, id), true
		}
	}
	return ids, nil
}
