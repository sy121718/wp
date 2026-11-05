package admindto

import (
	"strings"

	adminenums "go_wp/internal/module/admin/enums"
)

// I18nWriteReq 是词条页面的标准表单输入，批量动作必须显式声明。
type I18nWriteReq struct {
	Action   string   `form:"action" binding:"required,oneof=create update delete bulkDelete"`
	Key      string   `form:"key"`
	Lang     string   `form:"lang"`
	Value    string   `form:"value"`
	Category string   `form:"category"`
	Remark   string   `form:"remark"`
	IDs      []string `form:"ids"`
}

// Validate 校验不同动作的基础必填字段，不改写词条内容。
func (req *I18nWriteReq) Validate() error {
	if req.Action == "bulkDelete" {
		return nil
	}
	if strings.TrimSpace(req.Key) == "" {
		return adminenums.Err(adminenums.ErrI18nKeyEmpty)
	}
	if strings.TrimSpace(req.Lang) == "" {
		return adminenums.Err(adminenums.ErrI18nLangEmpty)
	}
	if req.Action != "delete" && strings.TrimSpace(req.Value) == "" {
		return adminenums.Err(adminenums.ErrI18nValueEmpty)
	}
	return nil
}

// I18nEditReq 按 key 与语言精确读取一条词条。
type I18nEditReq struct {
	Key  string `form:"key" binding:"required"`
	Lang string `form:"lang" binding:"required"`
}
