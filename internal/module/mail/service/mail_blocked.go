package mailservice

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/facing"
)

// mailBlockedError 是一次批量写被整体拒绝时的事实：
// 主消息只取一条公共文案（单条请求取该条的具体业务原因），
// 逐条 id / 名称 / 原因交给共享出口放进 data.items。
type mailBlockedError struct {
	key   string
	items []facing.BlockedItem
}

func (e *mailBlockedError) Error() string { return e.key }

func (e *mailBlockedError) Facing() (string, string) { return e.key, "" }

func (e *mailBlockedError) HTTPStatus() int { return blockedStatus(e.items) }

func (e *mailBlockedError) BlockedItems() []facing.BlockedItem { return e.items }

// rejectBatch 构造整批拒绝事实：
// 单条请求且只有它失败时主消息给该条的具体业务原因，其余情况给公共整批文案；
// 两种形状都保留 items，状态按原因区分缺失 404 / 冲突 409 / 参数 400。
func rejectBatch(requested int, items []facing.BlockedItem) error {
	if len(items) == 0 {
		return nil
	}
	key := mailenums.ErrBatchReject
	if requested == 1 && len(items) == 1 {
		key = items[0].ReasonKey
	}
	return &mailBlockedError{key: key, items: items}
}

// blockedStatus 从拒绝原因推导 HTTP 状态：冲突优先于缺失，其余按参数错误。
func blockedStatus(items []facing.BlockedItem) int {
	status := http.StatusBadRequest
	for _, item := range items {
		switch mailenums.StatusFor(item.ReasonKey) {
		case http.StatusConflict:
			return http.StatusConflict
		case http.StatusNotFound:
			status = http.StatusNotFound
		}
	}
	return status
}

func blockedIDItem(id uint64, reasonKey string) facing.BlockedItem {
	return facing.BlockedItem{ID: strconv.FormatUint(id, 10), ReasonKey: reasonKey}
}

// missingIDs 返回请求里没有命中任何行的 id，保持请求顺序。
func missingIDs[T any](ids []uint64, rows []T, idOf func(T) uint64) []uint64 {
	found := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		found[idOf(row)] = struct{}{}
	}
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

func blockedIDs(ids []uint64, reasonKey string) []facing.BlockedItem {
	items := make([]facing.BlockedItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, blockedIDItem(id, reasonKey))
	}
	return items
}

// blockedRow 描述按行校验出的拒绝项；name 用于定位行，reasonKey 决定对外原因。
func blockedRow(id, name, reasonKey string) facing.BlockedItem {
	return facing.BlockedItem{ID: id, Name: strings.TrimSpace(name), ReasonKey: reasonKey}
}

// sortedKeys 按字典序返回 map 的键，让提交后的触发顺序稳定。
func sortedKeys[T any](m map[string][]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
