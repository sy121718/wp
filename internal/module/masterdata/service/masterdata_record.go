// masterdata_record.go — 变更记录的写入（append-only 的唯一入口，issue #19 验收 1/3）。
//
// 写入流程只有一条：调用方给「改前 / 改后」快照 → 本模块做字段级 diff → 逐字段落一行。
// 三个刻意的语义：
//
//   - **逐字段一行**：一次写操作改了三个字段就是三行。这样「按字段查历史」天然成立，
//     不用去解析某一行的 JSON（接口与页面的筛选维度直接落在列上）；
//   - **只写真正变化的字段**：old == new 一律不落行。审计表的噪声会直接吃掉它的可信度
//     （「这条记录到底改没改」看不出来）；
//   - **新增 / 删除同样逐字段落行**：新增时 old 为空、删除时 new 为空，
//     于是任何实体的历史都是同一形状的字段级时间线，读取侧不需要分支。
package masterdataservice

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatamodel "go_wp/internal/module/masterdata/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RecordChanges 追加变更记录（append-only）。
//
// 输入为空、或某条输入一个字段都没变时，不写任何行（返回 nil）——
// 「打开表单什么都没改就点保存」不应该在审计里留下痕迹。
func (s *Service) RecordChanges(ctx context.Context, inputs []*masterdatacontract.ChangeInput) (err error) {
	rows, err := buildChangeRowsBatch(inputs)
	if err != nil || len(rows) == 0 {
		return err
	}
	return s.m.Append(ctx, rows)
}

// RecordChangesTx 在外部事务内追加变更记录。
func (s *Service) RecordChangesTx(ctx context.Context, tx *gorm.DB, inputs []*masterdatacontract.ChangeInput) (err error) {
	if tx == nil {
		return s.RecordChanges(ctx, inputs)
	}
	rows, err := buildChangeRowsBatch(inputs)
	if err != nil || len(rows) == 0 {
		return err
	}
	return s.m.AppendTx(tx, rows)
}

func buildChangeRowsBatch(inputs []*masterdatacontract.ChangeInput) (rows []*masterdatamodel.ChangeEntity, err error) {
	now := time.Now().UTC()
	rows = make([]*masterdatamodel.ChangeEntity, 0, len(inputs))
	for _, in := range inputs {
		if in == nil {
			continue
		}
		built, berr := buildChangeRows(in, now)
		if berr != nil {
			return nil, berr
		}
		rows = append(rows, built...)
	}
	return rows, nil
}

// buildChangeRows 把一条变更输入展开成字段级记录行。
func buildChangeRows(in *masterdatacontract.ChangeInput, now time.Time) (rows []*masterdatamodel.ChangeEntity, err error) {
	if strings.TrimSpace(in.ProjectID) == "" {
		return nil, errors.New(masterdataenums.ErrProjectRequired)
	}
	entityType := strings.TrimSpace(in.EntityType)
	if !masterdataenums.IsValidEntityType(entityType) {
		return nil, errors.New(masterdataenums.ErrEntityTypeInvalid)
	}
	action := strings.TrimSpace(in.Action)
	if !masterdataenums.IsValidAction(action) {
		return nil, errors.New(masterdataenums.ErrActionInvalid)
	}
	entityID := strings.TrimSpace(in.EntityID)
	if entityID == "" {
		return nil, errors.New(masterdataenums.ErrEntityIDRequired)
	}
	if _, perr := uuid.Parse(entityID); perr != nil {
		return nil, errors.New(masterdataenums.ErrInvalidParam)
	}
	for _, field := range unionFields(in.Before, in.After) {
		oldValue := strings.TrimSpace(in.Before[field])
		newValue := strings.TrimSpace(in.After[field])
		if oldValue == newValue {
			continue
		}
		// 新增时「创建出来就是空」的字段不算变更；删除时「删之前也没值」的字段同理。
		if action == masterdataenums.ActionCreate && newValue == "" {
			continue
		}
		if action == masterdataenums.ActionDelete && oldValue == "" {
			continue
		}
		rows = append(rows, &masterdatamodel.ChangeEntity{
			ID: uuid.NewString(), ProjectID: strings.TrimSpace(in.ProjectID),
			EntityType: entityType, EntityID: entityID,
			EntityLabel: truncateValue(strings.TrimSpace(in.EntityLabel)),
			Action:      action, Field: field,
			OldValue: truncateValue(oldValue), NewValue: truncateValue(newValue),
			Origin: strings.TrimSpace(in.Origin), OperatorID: strings.TrimSpace(in.OperatorID),
			CreatedAt: now,
		})
	}
	return rows, nil
}

// unionFields 两侧字段名的并集，**按字段名排序**返回。
//
// 排序是为了确定性：同一份输入每次产出的行顺序一致，后台列表与测试都不依赖 map 遍历顺序。
func unionFields(before, after masterdatacontract.FieldSnapshot) (fields []string) {
	seen := map[string]bool{}
	for field := range before {
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		fields = append(fields, field)
	}
	for field := range after {
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// truncateValue 超长值截断（按字符数，不切断 UTF-8 字节序列）。
func truncateValue(value string) string {
	runes := []rune(value)
	if len(runes) <= maxFieldValueLen {
		return value
	}
	return string(runes[:maxFieldValueLen]) + truncatedSuffix
}
