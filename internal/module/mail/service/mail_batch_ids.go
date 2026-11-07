package mailservice

import (
	"net/http"
	"strconv"
	"strings"

	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"

	mailenums "go_wp/internal/module/mail/enums"
)

// InvalidBatchIDsError 是表单里含非法批量 id 时的整批拒绝事实。
//
// 主消息只取一条公共文案，逐条非法 id 交给共享出口放进 data.items。
type InvalidBatchIDsError struct {
	ids []string
}

func (e *InvalidBatchIDsError) Error() string { return mailenums.ErrBatchReject }

func (e *InvalidBatchIDsError) Facing() (string, string) { return mailenums.ErrBatchReject, "" }

func (e *InvalidBatchIDsError) HTTPStatus() int { return http.StatusBadRequest }

func (e *InvalidBatchIDsError) BlockedItems() []facing.BlockedItem {
	items := make([]facing.BlockedItem, 0, len(e.ids))
	for _, id := range e.ids {
		items = append(items, facing.BlockedItem{ID: id, ReasonKey: mailenums.ErrInvalidParam})
	}
	return items
}

// NormalizeBatchIDs 归一批表单 id：去重、校验上限、逐条转成整数。
//
// 归一（身份去重与上限）属于业务规则，归 service；控制器只交表单原始值。
func NormalizeBatchIDs(raw []string) ([]uint64, error) {
	cleaned := make([]string, 0, len(raw))
	for _, item := range raw {
		if v := strings.TrimSpace(item); v != "" {
			cleaned = append(cleaned, v)
		}
	}
	normalized, err := bulkids.Normalize(cleaned)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	ids := make([]uint64, 0, len(normalized))
	invalid := make([]string, 0)
	for _, item := range normalized {
		id, perr := strconv.ParseUint(item, 10, 64)
		if perr != nil || id == 0 {
			invalid = append(invalid, item)
			continue
		}
		ids = append(ids, id)
	}
	if len(invalid) > 0 {
		return nil, &InvalidBatchIDsError{ids: invalid}
	}
	return ids, nil
}
