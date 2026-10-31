package publishrecord

import (
	"context"
	"go_wp/pkg/utils"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

type Port struct {
	records  presentationcontract.PublishRecordPort
	attempts presentationcontract.PublishAttemptPort
}

func New(records presentationcontract.PublishRecordPort) *Port {
	if records == nil {
		return nil
	}
	return &Port{records: records}
}

func NewWithAttempts(records presentationcontract.PublishRecordPort,
	attempts presentationcontract.PublishAttemptPort) *Port {
	p := New(records)
	if p == nil && attempts == nil {
		return nil
	}
	if p == nil {
		p = &Port{}
	}
	p.attempts = attempts
	return p
}

func (p *Port) PublishAttempts(ctx context.Context, projectID, productID string, limit int) (*productdto.PublishAttemptListResp, error) {
	if p == nil || p.attempts == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	page, err := p.attempts.PublishAttempts(ctx, projectID, productcontract.EntityTypeProduct, productID, limit)
	if err != nil {
		return nil, err
	}
	res := &productdto.PublishAttemptListResp{
		Rows:    make([]productdto.PublishAttemptRowResp, 0, len(page.Rows)),
		Total:   page.Total,
		HasMore: page.HasMore,
	}
	for _, r := range page.Rows {
		res.Rows = append(res.Rows, productdto.PublishAttemptRowResp{
			ID:              r.ID,
			CreatedAt:       utils.NewJSONTime(r.CreatedAt),
			Trigger:         r.Trigger,
			Outcome:         r.Outcome,
			Langs:           r.Langs,
			Attempts:        r.Attempts,
			ArtifactVersion: r.ArtifactVersion,
			ArtifactHash:    r.ArtifactHash,
			OperatorName:    r.OperatorName,
			DurationMs:      r.DurationMs,
		})
	}
	return res, nil
}

func (p *Port) Langs(ctx context.Context, projectID string, entityIDs []string) (map[string][]productdto.PublishRecordLangResp, error) {
	if p == nil {
		return map[string][]productdto.PublishRecordLangResp{}, nil
	}
	rows, err := p.records.PublishRecords(ctx, projectID, productcontract.EntityTypeProduct, entityIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]productdto.PublishRecordLangResp, len(rows))
	for _, r := range rows {
		out[r.EntityID] = append(out[r.EntityID], productdto.PublishRecordLangResp{
			Lang:            r.Lang,
			OnlinePath:      r.ActivePath,
			Online:          r.Active,
			Stale:           r.Stale,
			PublishedAt:     r.PublishedAt,
			ArtifactVersion: r.ArtifactVersion,
			ArtifactHash:    r.ArtifactHash,
		})
	}
	return out, nil
}
