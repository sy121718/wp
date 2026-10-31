package inventorydto

import (
	"net/url"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

type ReasonFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Builtin   string `form:"builtin"`
	Code      string `form:"code"`
	Name      string `form:"name"`
	Direction string `form:"direction"`
	Status    string `form:"status"`
	Sort      string `form:"sort"`

	submitted map[string]bool
}

var reasonFormKeys = []string{"code", "name", "direction", "status", "sort"}

func (r *ReasonFormReq) BindForm(values url.Values) error {
	r.submitted = make(map[string]bool, len(reasonFormKeys))
	for _, key := range reasonFormKeys {
		_, r.submitted[key] = values[key]
	}
	return nil
}

func (r *ReasonFormReq) Submitted(key string) bool { return r.submitted[key] }

func (r *ReasonFormReq) CreateReq() *CreateReasonReq {
	return &CreateReasonReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		Code:      strings.TrimSpace(r.Code),
		Name:      strings.TrimSpace(r.Name),
		Direction: strings.TrimSpace(r.Direction),
		Sort:      formInt(r.Sort, 0),
	}
}

func (r *ReasonFormReq) UpdateReq() *UpdateReasonReq {
	req := &UpdateReasonReq{ProjectID: strings.TrimSpace(r.ProjectID), ID: strings.TrimSpace(r.ID)}
	if name := strings.TrimSpace(r.Name); r.Submitted("name") {
		req.Name = &name
	}
	if status := strings.TrimSpace(r.Status); status != "" {
		req.Status = &status
	}
	if sortRaw := strings.TrimSpace(r.Sort); r.Submitted("sort") && sortRaw != "" {
		sort := formInt(sortRaw, 0)
		req.Sort = &sort
	}
	return req
}

type ReasonBulkFormReq struct {
	ProjectID string   `form:"projectId"`
	Status    string   `form:"status"`
	IDs       []string `form:"ids"`
}

func (r *ReasonBulkFormReq) ValidStatus() bool {
	status := strings.TrimSpace(r.Status)
	return status == inventoryenums.StatusActive || status == inventoryenums.StatusDisabled
}

func (r *ReasonBulkFormReq) StatusValue() string { return strings.TrimSpace(r.Status) }
