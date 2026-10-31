package masterdatacontract

import (
	"bytes"
	"context"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"

	masterdatadto "go_wp/internal/module/masterdata/dto"
	"go_wp/pkg/money"

	"gorm.io/gorm"
)

type FieldSnapshot map[string]string

type ChangeInput struct {
	ProjectID string

	EntityType string

	EntityID string

	EntityLabel string

	Action string

	Origin string

	OperatorID string
	Before     FieldSnapshot
	After      FieldSnapshot
}

type MasterDataService interface {
	RecordChanges(ctx context.Context, inputs []*ChangeInput) (err error)

	RecordChangesTx(ctx context.Context, tx *gorm.DB, inputs []*ChangeInput) (err error)

	ListChanges(ctx context.Context, req *masterdatadto.ListChangeReq) (list []*masterdatadto.ChangeResp, err error)

	CountChanges(ctx context.Context, req *masterdatadto.ListChangeReq) (n int64, err error)

	ListEntities(ctx context.Context, req *masterdatadto.ListEntityReq) (list []*masterdatadto.EntityHistoryResp, err error)

	CountEntities(ctx context.Context, req *masterdatadto.ListEntityReq) (n int64, err error)

	EntityTimeline(ctx context.Context, req *masterdatadto.EntityTimelineReq) (res *masterdatadto.EntityTimelineResp, err error)
}

func FormatPrice(v float64) string {
	return money.FormatAudit(v)
}

func FormatPricePtr(v *float64) string {
	if v == nil {
		return ""
	}
	return FormatPrice(*v)
}

func FormatBool(v bool) string {
	return strconv.FormatBool(v)
}

func FormatInt(v int) string {
	return strconv.Itoa(v)
}

func FormatStringPtr(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func FormatJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

func NewSnapshot(pairs ...string) FieldSnapshot {
	out := FieldSnapshot{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i]] = pairs[i+1]
	}
	return out
}
