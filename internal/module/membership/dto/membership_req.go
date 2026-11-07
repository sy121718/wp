package membershipdto

import (
	"strconv"
	"strings"

	membershipenums "go_wp/internal/module/membership/enums"
)

type ListTiersReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
}

type GetTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

type EntitlementReq struct {
	Kind string `json:"kind" form:"kind"`

	ValueInt int64 `json:"valueInt" form:"valueInt"`
}

type CreateTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Name      string `json:"name" form:"name"`

	SortOrder int `json:"sortOrder" form:"sortOrder"`

	ThresholdAmount int64 `json:"thresholdAmount" form:"thresholdAmount"`

	IsDefault bool   `json:"isDefault" form:"isDefault"`
	Remark    string `json:"remark" form:"remark"`

	Entitlements []EntitlementReq `json:"entitlements" form:"entitlements"`
}

type UpdateTierReq struct {
	ProjectID string  `json:"projectId" form:"projectId"`
	TierID    int64   `json:"tierId" form:"tierId"`
	Name      *string `json:"name" form:"name"`
	SortOrder *int    `json:"sortOrder" form:"sortOrder"`

	ThresholdAmount *int64  `json:"thresholdAmount" form:"thresholdAmount"`
	IsDefault       *bool   `json:"isDefault" form:"isDefault"`
	Remark          *string `json:"remark" form:"remark"`
}

type DeleteTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

type SaveEntitlementsReq struct {
	ProjectID string           `json:"projectId" form:"projectId"`
	TierID    int64            `json:"tierId" form:"tierId"`
	Items     []EntitlementReq `json:"items" form:"items"`
}

type ResolveReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	UserID uint64 `json:"userId" form:"userId"`
}

type ListAssignmentsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	TierID int64 `json:"tierId" form:"tierId"`

	Source string `json:"source" form:"source"`

	UserID uint64 `json:"userId" form:"userId"`
	Page   int    `json:"page" form:"page"`
	Size   int    `json:"size" form:"size"`
}

type CountAssignmentsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
	Source    string `json:"source" form:"source"`
	UserID    uint64 `json:"userId" form:"userId"`
}

type AssignManualReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	UserID    uint64 `json:"userId" form:"userId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

type UnlockManualReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	UserID    uint64 `json:"userId" form:"userId"`
}

type RecalcProjectReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
}

// 页面表单：普通控件提交，服务端负责把「门槛元」与「权益勾选」映射成业务对象。
// 复杂结构（EntitlementReq 清单）不从这里走 —— 表单只有真实控件能表达的字段。

type TierFormReq struct {
	ProjectID       string `form:"projectId"`
	TierID          string `form:"tierId"`
	Name            string `form:"name"`
	ThresholdYuan   string `form:"thresholdYuan"`
	SortOrder       string `form:"sortOrder"`
	IsDefault       string `form:"isDefault"`
	Remark          string `form:"remark"`
	FreeShipping    string `form:"freeShipping"`
	DiscountPercent string `form:"discountPercent"`
}

type EntitlementFormReq struct {
	ProjectID       string `form:"projectId"`
	TierID          string `form:"tierId"`
	FreeShipping    string `form:"freeShipping"`
	DiscountPercent string `form:"discountPercent"`
}

type AssignFormReq struct {
	ProjectID string `form:"projectId"`
	UserID    string `form:"userId"`
	TierID    string `form:"tierId"`
}

type UnlockFormReq struct {
	ProjectID string `form:"projectId"`
	UserID    string `form:"userId"`
}

type DeleteTierFormReq struct {
	ProjectID string `form:"projectId"`
	TierID    string `form:"tierId"`
}

// ParseIntField 把表单里的整数字串转成数值；空串或非法值返回错误。
func ParseIntField(text string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return value, nil
}

// ParseYuan 把「元」的字符串按十进制语法精确换算成分。
// 不走浮点：金额一旦经过 float64 就会在边界上漂移。
func ParseYuan(text string) (int64, error) {
	raw := strings.ReplaceAll(strings.TrimSpace(text), ",", "")
	if raw == "" {
		return 0, nil
	}
	neg := false
	if strings.HasPrefix(raw, "-") {
		neg, raw = true, raw[1:]
	}
	whole, frac, hasFrac := strings.Cut(raw, ".")
	if len(frac) > 2 || !hasFrac && strings.Contains(raw, ".") {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
		}
	}
	if whole == "" {
		whole = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	cents, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	if neg {
		cents = -cents
	}
	return cents, nil
}


// UpdateTierReq 把表单投影成 service 入参：只携带页面真正提交的字段。
func (f *TierFormReq) UpdateReq() (*UpdateTierReq, error) {
	tierID, err := ParseIntField(f.TierID)
	if err != nil {
		return nil, err
	}
	req := &UpdateTierReq{ProjectID: f.ProjectID, TierID: tierID}
	if strings.TrimSpace(f.Name) != "" {
		name := f.Name
		req.Name = &name
	}
	if strings.TrimSpace(f.Remark) != "" {
		remark := f.Remark
		req.Remark = &remark
	}
	if strings.TrimSpace(f.SortOrder) != "" {
		sortOrder, perr := ParseIntField(f.SortOrder)
		if perr != nil {
			return nil, perr
		}
		v := int(sortOrder)
		req.SortOrder = &v
	}
	if strings.TrimSpace(f.ThresholdYuan) != "" {
		threshold, perr := ParseYuan(f.ThresholdYuan)
		if perr != nil {
			return nil, perr
		}
		req.ThresholdAmount = &threshold
	}
	if strings.TrimSpace(f.IsDefault) != "" {
		isDefault := true
		req.IsDefault = &isDefault
	}
	return req, nil
}

// CreateTierReq 把表单投影成 service 入参。
func (f *TierFormReq) CreateReq() (*CreateTierReq, error) {
	threshold, err := ParseYuan(f.ThresholdYuan)
	if err != nil {
		return nil, err
	}
	sortOrder := int64(0)
	if strings.TrimSpace(f.SortOrder) != "" {
		if sortOrder, err = ParseIntField(f.SortOrder); err != nil {
			return nil, err
		}
	}
	items := make([]EntitlementReq, 0, 2)
	if strings.TrimSpace(f.FreeShipping) != "" {
		items = append(items, EntitlementReq{Kind: membershipenums.KindFreeShipping, ValueInt: 1})
	}
	if strings.TrimSpace(f.DiscountPercent) != "" {
		value, perr := strconv.ParseInt(strings.TrimSpace(f.DiscountPercent), 10, 64)
		if perr != nil {
			value = -1
		}
		items = append(items, EntitlementReq{Kind: membershipenums.KindDiscount, ValueInt: value})
	}
	return &CreateTierReq{
		ProjectID:       f.ProjectID,
		Name:            f.Name,
		SortOrder:       int(sortOrder),
		ThresholdAmount: threshold,
		IsDefault:       strings.TrimSpace(f.IsDefault) != "",
		Remark:          f.Remark,
		Entitlements:    items,
	}, nil
}

// DeleteReq 把删除表单投影成 service 入参。
func (f *DeleteTierFormReq) DeleteReq() (*DeleteTierReq, error) {
	tierID, err := ParseIntField(f.TierID)
	if err != nil {
		return nil, err
	}
	return &DeleteTierReq{ProjectID: f.ProjectID, TierID: tierID}, nil
}

// SaveReq 把权益表单投影成 service 入参。
func (f *EntitlementFormReq) SaveReq() (*SaveEntitlementsReq, error) {
	tierID, err := ParseIntField(f.TierID)
	if err != nil {
		return nil, err
	}
	items := make([]EntitlementReq, 0, 2)
	if strings.TrimSpace(f.FreeShipping) != "" {
		items = append(items, EntitlementReq{Kind: membershipenums.KindFreeShipping, ValueInt: 1})
	}
	if strings.TrimSpace(f.DiscountPercent) != "" {
		value, perr := strconv.ParseInt(strings.TrimSpace(f.DiscountPercent), 10, 64)
		if perr != nil {
			value = -1
		}
		items = append(items, EntitlementReq{Kind: membershipenums.KindDiscount, ValueInt: value})
	}
	return &SaveEntitlementsReq{ProjectID: f.ProjectID, TierID: tierID, Items: items}, nil
}

// AssignReq 把手工指定表单投影成 service 入参。
func (f *AssignFormReq) AssignReq() (*AssignManualReq, error) {
	userID, err := ParseUintField(f.UserID)
	if err != nil {
		return nil, err
	}
	tierID, err := ParseIntField(f.TierID)
	if err != nil {
		return nil, err
	}
	return &AssignManualReq{ProjectID: f.ProjectID, UserID: userID, TierID: tierID}, nil
}

// UnlockReq 把解除锁定表单投影成 service 入参。
func (f *UnlockFormReq) UnlockReq() (*UnlockManualReq, error) {
	userID, err := ParseUintField(f.UserID)
	if err != nil {
		return nil, err
	}
	return &UnlockManualReq{ProjectID: f.ProjectID, UserID: userID}, nil
}

// ParseUintField 把表单里的无符号整数字串转成数值；空串或非法值返回错误。
func ParseUintField(text string) (uint64, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return value, nil
}
