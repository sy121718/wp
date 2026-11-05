package publishrecord

import (
	"context"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/utils"
)

type Port struct {
	attempts presentationcontract.PublishAttemptPort
}

// NewWithAttempts 装配发布历史读口；没有尝试端口时返回 nil，调用处按「未装配」处理。
func NewWithAttempts(attempts presentationcontract.PublishAttemptPort) *Port {
	if attempts == nil {
		return nil
	}
	return &Port{attempts: attempts}
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
