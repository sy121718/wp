package productdto

import (
	"math"
	"net/url"
	"strings"

	productenums "go_wp/internal/module/product/enums"
)

type AddRatingReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`

	Score *float64 `json:"score" form:"score" binding:"required"`

	Source string `json:"source" form:"source"`

	OperatorID string `json:"-" form:"-"`
}

type ListRatingsReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

type DeleteRatingReq struct {
	ID        string `json:"id" form:"id" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

// Validate 是这张表单的业务校验：分值必须出现且落在 0..最大分之间。
//
// 表单绑定会把空串绑成 0，所以「没拿到值」与「显式 0 分」必须分开判记：
// 前者是零值指针（或空串提交），后者是合法输入。越界、NaN、Inf 同样在这里拒绝，
// 不靠 service 兜底 —— 否则非法输入会先落到业务层。
func (r *AddRatingReq) Validate() error {
	if r.Score == nil {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	if math.IsNaN(*r.Score) || math.IsInf(*r.Score, 0) || *r.Score < 0 || *r.Score > maxRatingScore {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	return nil
}

// maxRatingScore 与 service 的边界保持一致（0..5）。
const maxRatingScore = 5

// ValidatePresence 在绑定前检查表单字段的「出现/清空」语义。
//
// 为什么必须在这里判：Gin 的 form 绑定会把空串绑成 0，绑定后的 req.Score 既可能来自
// 「显式 0 分」也可能是「空串」。空串必须拒绝，而这条信息只有原始表单里才有。
func (r *AddRatingReq) ValidatePresence(vals url.Values) error {
	raw, submitted := vals["score"]
	if !submitted {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	for _, v := range raw {
		if strings.TrimSpace(v) == "" {
			return productenums.Err(productenums.ErrInvalidParam)
		}
	}
	return nil
}
