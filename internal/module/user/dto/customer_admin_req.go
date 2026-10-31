package userdto

import (
	"bytes"
	"strconv"

	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/bulkids"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

const (
	EmailVerifiedAll = 0
	EmailVerifiedYes = 1
	EmailVerifiedNo  = 2
)

type CustomerListReq struct {
	Keyword string

	Status int

	EmailVerified int

	RegisteredFrom *utils.JSONTime
	RegisteredTo   *utils.JSONTime

	LockedOnly bool

	UserIDs []int64
	Offset  int
	Limit   int
}

const CustomerStatusAll = -1

type CustomerStatusReq struct {
	CustomerID uint64
	Status     int
}

type CustomerStatusSetReq struct {
	CustomerID uint64 `json:"customerId" form:"customerId"`
	Status     *int   `json:"status" form:"status"`
}

type CustomerUnlockReq struct {
	CustomerID uint64 `json:"customerId" form:"customerId"`
}

type CustomerBulkUnlockReq struct {
	IDs []uint64 `json:"ids,case:strict" form:"ids" binding:"required,min=1"`
}

func (r *CustomerBulkUnlockReq) UnmarshalJSON(data []byte) error {
	type plain CustomerBulkUnlockReq
	var decoded plain
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil || decoded.IDs == nil {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	*r = CustomerBulkUnlockReq(decoded)
	return nil
}

func (r CustomerBulkUnlockReq) CustomerBulkValues() ([]uint64, int, error) {
	ids, err := normalizeCustomerBulkIDs(r.IDs)
	return ids, 0, err
}

type CustomerBulkStatusReq struct {
	IDs    []uint64 `json:"ids,case:strict" form:"ids" binding:"required,min=1"`
	Status *int     `json:"status,case:strict" form:"status" binding:"required"`
}

func (r *CustomerBulkStatusReq) UnmarshalJSON(data []byte) error {

	var fields struct {
		IDs    json.RawMessage `json:"ids,case:strict"`
		Status json.RawMessage `json:"status,case:strict"`
	}
	if err := json.Unmarshal(data, &fields, json.RejectUnknownMembers(true)); err != nil ||
		bytes.Equal(bytes.TrimSpace(fields.IDs), []byte("null")) || bytes.Equal(bytes.TrimSpace(fields.Status), []byte("null")) {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	var decoded CustomerBulkStatusReq
	if err := json.Unmarshal(fields.IDs, &decoded.IDs); err != nil {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	if err := json.Unmarshal(fields.Status, &decoded.Status); err != nil ||
		decoded.Status == nil || (*decoded.Status != userenums.StatusActive && *decoded.Status != userenums.StatusDisabled) {
		return userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	*r = decoded
	return nil
}

func (r CustomerBulkStatusReq) CustomerBulkValues() ([]uint64, int, error) {
	if r.Status == nil {
		return nil, 0, userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	ids, err := normalizeCustomerBulkIDs(r.IDs)
	return ids, *r.Status, err
}

func normalizeCustomerBulkIDs(input []uint64) ([]uint64, error) {
	idsText := make([]string, len(input))
	for i, id := range input {
		idsText[i] = strconv.FormatUint(id, 10)
	}
	normalized, err := bulkids.Normalize(idsText)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	ids := make([]uint64, len(normalized))
	for i, id := range normalized {
		ids[i], _ = strconv.ParseUint(id, 10, 64)
	}
	return ids, nil
}
