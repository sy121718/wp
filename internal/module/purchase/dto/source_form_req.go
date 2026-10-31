package purchasedto

import (
	"strconv"
	"strings"

	purchaseenums "go_wp/internal/module/purchase/enums"
	json "go_wp/pkg/jsonx"
)

type SourceFormReq struct {
	ProjectID    string `form:"projectId"`
	ID           string `form:"id"`
	Code         string `form:"code"`
	Name         string `form:"name"`
	Type         string `form:"type"`
	Status       string `form:"status"`
	RelatedParty string `form:"relatedParty"`
	SettlePrice  string `form:"settlePrice"`
	Sort         string `form:"sort"`
	Config       string `form:"config"`
}

func (r *SourceFormReq) Related() *bool {
	switch strings.ToLower(strings.TrimSpace(r.RelatedParty)) {
	case "true", "1":
		yes := true
		return &yes
	case "false", "0":
		no := false
		return &no
	default:
		return nil
	}
}

func (r *SourceFormReq) Settle() (*float64, error) {
	raw := strings.TrimSpace(r.SettlePrice)
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceSettleInvalid)
	}
	return &parsed, nil
}

func (r *SourceFormReq) ConfigValue() json.RawMessage {
	raw := strings.TrimSpace(r.Config)
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}

func (r *SourceFormReq) CreateReq() (*CreateSourceReq, error) {
	price, err := r.Settle()
	if err != nil {
		return nil, err
	}
	return &CreateSourceReq{
		ProjectID:    strings.TrimSpace(r.ProjectID),
		Code:         strings.TrimSpace(r.Code),
		Name:         strings.TrimSpace(r.Name),
		Type:         strings.TrimSpace(r.Type),
		Status:       strings.TrimSpace(r.Status),
		RelatedParty: r.Related(),
		Config:       r.ConfigValue(),
		Sort:         formInt(r.Sort, 0),
		SettlePrice:  price,
	}, nil
}

func (r *SourceFormReq) UpdateReq() (*UpdateSourceReq, error) {
	code, name := strings.TrimSpace(r.Code), strings.TrimSpace(r.Name)
	sourceType, status := strings.TrimSpace(r.Type), strings.TrimSpace(r.Status)
	sort := formInt(r.Sort, 0)
	req := &UpdateSourceReq{
		ID:           strings.TrimSpace(r.ID),
		Code:         &code,
		Name:         &name,
		Type:         &sourceType,
		Status:       &status,
		Sort:         &sort,
		RelatedParty: r.Related(),
		Config:       r.ConfigValue(),
	}
	price, err := r.Settle()
	if err != nil {
		return nil, err
	}
	if price == nil {
		req.ClearSettlePrice = true
	} else {
		req.SettlePrice = price
	}
	return req, nil
}

type SourceDeleteFormReq struct {
	ID string `form:"id"`
}

type SourceBulkFormReq struct {
	ProjectID string   `form:"projectId"`
	IDs       []string `form:"ids"`
}
