package mediamodel

import (
	"context"
	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"
)

type AttachmentRef struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

type extraRefs struct {
	Refs []AttachmentRef `json:"refs"`
}

func refMatchJSON(kind string, refID string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"refs": []AttachmentRef{{Kind: kind, ID: refID}},
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (m *AttachmentModel) ListRefs(ctx context.Context, id uint64) ([]AttachmentRef, error) {
	var raw *string
	if err := m.attrDB(ctx).Select("extra_info::text").Where("id = ?", id).Scan(&raw).Error; err != nil {
		return nil, err
	}
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var parsed extraRefs
	if err := json.Unmarshal([]byte(*raw), &parsed); err != nil {

		return nil, nil
	}
	return parsed.Refs, nil
}

func (m *AttachmentModel) HasRef(ctx context.Context, id uint64, kind string, refID string) (bool, error) {
	match, err := refMatchJSON(kind, refID)
	if err != nil {
		return false, err
	}
	var n int64

	if err := m.attrDB(ctx).Where("id = ? AND extra_info @> ?::jsonb", id, match).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (m *AttachmentModel) AddRef(ctx context.Context, id uint64, ref AttachmentRef) (bool, error) {
	match, err := refMatchJSON(ref.Kind, ref.ID)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(ref)
	if err != nil {
		return false, err
	}

	const setRefs = `jsonb_set(
        CASE WHEN jsonb_typeof(extra_info) = 'object' THEN extra_info ELSE '{}'::jsonb END, '{refs}',
        coalesce(
            CASE WHEN jsonb_typeof(CASE WHEN jsonb_typeof(extra_info) = 'object' THEN extra_info ELSE '{}'::jsonb END -> 'refs') = 'array'
                 THEN (CASE WHEN jsonb_typeof(extra_info) = 'object' THEN extra_info ELSE '{}'::jsonb END) -> 'refs' END,
            '[]'::jsonb
        ) || ?::jsonb, true)`
	res := m.attrDB(ctx).
		Where("id = ? AND (extra_info IS NULL OR NOT (extra_info @> ?::jsonb))", id, match).
		Update("extra_info", gorm.Expr(setRefs, string(payload)))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (m *AttachmentModel) RemoveRef(ctx context.Context, id uint64, kind string, refID string) (bool, error) {
	match, err := refMatchJSON(kind, refID)
	if err != nil {
		return false, err
	}
	const dropRef = `jsonb_set(extra_info, '{refs}', coalesce(
        (SELECT jsonb_agg(e) FROM jsonb_array_elements(extra_info -> 'refs') AS e
          WHERE NOT (e ->> 'kind' = ? AND e ->> 'id' = ?)), '[]'::jsonb), true)`
	res := m.attrDB(ctx).
		Where("id = ? AND extra_info @> ?::jsonb", id, match).
		Update("extra_info", gorm.Expr(dropRef, kind, refID))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (m *AttachmentModel) ReplaceRefs(ctx context.Context, id uint64, refs []AttachmentRef) error {
	if refs == nil {
		refs = []AttachmentRef{}
	}
	payload, err := json.Marshal(refs)
	if err != nil {
		return err
	}

	const setRefs = `jsonb_set(
        CASE WHEN jsonb_typeof(extra_info) = 'object' THEN extra_info ELSE '{}'::jsonb END,
        '{refs}', ?::jsonb, true)`
	return m.attrDB(ctx).Where("id = ?", id).Update("extra_info", gorm.Expr(setRefs, string(payload))).Error
}

func (m *AttachmentModel) ListIDsByRef(ctx context.Context, kind string, refID string) ([]uint64, error) {
	match, err := refMatchJSON(kind, refID)
	if err != nil {
		return nil, err
	}
	var ids []uint64
	if err := m.attrDB(ctx).
		Where("extra_info @> ?::jsonb", match).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}
