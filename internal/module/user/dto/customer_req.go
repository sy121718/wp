package userdto

import (
	"strconv"
	"strings"

	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/bulkids"
)

// 客户写端点的页面表单：页面控件只提交页面上的字段，
// 批量动作与单条动作共用同一批勾选框 / 同一个隐藏表单。

type CustomerStatusForm struct {
	CustomerID string `form:"customerId"`
	Status     string `form:"status"`
}

type CustomerUnlockForm struct {
	CustomerID string `form:"customerId"`
}

type CustomerBulkForm struct {
	IDs    []string `form:"ids"`
	Status string   `form:"status"`
}

// CustomerStatusValues 把表单字符串转成 service 需要的数值；
// 空值或非法值一律按参数不合法处理，不退化成 0 这类真实状态。
func (f CustomerStatusForm) CustomerStatusValues() (uint64, int, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(f.CustomerID), 10, 64)
	if err != nil || id == 0 {
		return 0, 0, userenums.Err(userenums.ErrInvalidParam)
	}
	status, err := strconv.Atoi(strings.TrimSpace(f.Status))
	if err != nil || (status != userenums.StatusActive && status != userenums.StatusDisabled) {
		return 0, 0, userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	return id, status, nil
}

func (f CustomerUnlockForm) CustomerUnlockValue() (uint64, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(f.CustomerID), 10, 64)
	if err != nil || id == 0 {
		return 0, userenums.Err(userenums.ErrInvalidParam)
	}
	return id, nil
}

func (f CustomerBulkForm) CustomerBulkIDs() ([]uint64, error) {
	ids, err := customerBulkUintIDs(f.IDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	return ids, nil
}

func (f CustomerBulkForm) CustomerBulkStatusValue() (int, error) {
	status, err := strconv.Atoi(strings.TrimSpace(f.Status))
	if err != nil || (status != userenums.StatusActive && status != userenums.StatusDisabled) {
		return 0, userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	return status, nil
}

// customerBulkUintIDs 去掉空白与重复项后转成整数；非法项让整批失败。
func customerBulkUintIDs(raw []string) ([]uint64, error) {
	seen := make(map[string]bool, len(raw))
	normalized := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	if len(normalized) > bulkids.MaxIDs {
		return nil, &bulkids.IDsError{Count: len(normalized), Max: bulkids.MaxIDs}
	}
	out := make([]uint64, 0, len(normalized))
	for _, item := range normalized {
		id, err := strconv.ParseUint(item, 10, 64)
		if err != nil || id == 0 {
			return nil, userenums.Err(userenums.ErrInvalidParam)
		}
		out = append(out, id)
	}
	return out, nil
}
