package masterdataservice

import (
	"context"
	"sort"
	"strings"
	"time"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	"go_wp/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *Service) RecordChanges(ctx context.Context, inputs []*masterdatacontract.ChangeInput) (err error) {
	rows, err := buildChangeRowsBatch(inputs)
	if err != nil || len(rows) == 0 {
		return err
	}
	return s.m.Append(ctx, rows)
}

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

func buildChangeRows(in *masterdatacontract.ChangeInput, now time.Time) (rows []*masterdatamodel.ChangeEntity, err error) {
	if strings.TrimSpace(in.ProjectID) == "" {
		return nil, masterdataenums.Err(masterdataenums.ErrProjectRequired)
	}
	entityType := strings.TrimSpace(in.EntityType)
	if !masterdataenums.IsValidEntityType(entityType) {
		return nil, masterdataenums.Err(masterdataenums.ErrEntityTypeInvalid)
	}
	action := strings.TrimSpace(in.Action)
	if !masterdataenums.IsValidAction(action) {
		return nil, masterdataenums.Err(masterdataenums.ErrActionInvalid)
	}
	entityID := strings.TrimSpace(in.EntityID)
	if entityID == "" {
		return nil, masterdataenums.Err(masterdataenums.ErrEntityIDRequired)
	}
	if _, perr := uuid.Parse(entityID); perr != nil {
		return nil, masterdataenums.Err(masterdataenums.ErrInvalidParam)
	}
	for _, field := range unionFields(in.Before, in.After) {
		oldValue := strings.TrimSpace(in.Before[field])
		newValue := strings.TrimSpace(in.After[field])
		if oldValue == newValue {
			continue
		}

		if action == masterdataenums.ActionCreate && newValue == "" {
			continue
		}
		if action == masterdataenums.ActionDelete && oldValue == "" {
			continue
		}
		rows = append(rows, &masterdatamodel.ChangeEntity{
			ID: utils.NewTimeOrderedID(), ProjectID: strings.TrimSpace(in.ProjectID),
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

func truncateValue(value string) string {
	runes := []rune(value)
	if len(runes) <= maxFieldValueLen {
		return value
	}
	return string(runes[:maxFieldValueLen]) + truncatedSuffix
}
