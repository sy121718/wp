package mediamodel

// media_ref_model.go — sys_attachment.extra_info.refs 的读写（迁移 067 的引用缓存）。
//
// 存储形态：extra_info 为 JSONB 对象，引用缓存放在其 refs 键下（数组）：
//   {"alt":"...","title":"...","refs":[{"kind":"page","id":"12","title":"首页"}]}
// 与 alt/title/description 同列共存——既有 UpdateAttachment 的合并写法
// （反序列化整个对象 → 只改 alt/title → 整体写回）天然保留 refs 键，无需改动。
//
// 全部更新走单条 SQL（jsonb_set / jsonb_agg 表达式），不做「读-改-写」：
// 构建期全量替换与后台元数据编辑并发时，各自只影响自己的键。

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
)

// AttachmentRef 一条引用记录（引用方类型 + 标识 + 标题）。
type AttachmentRef struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// extraRefs 只用于从 extra_info 中解出 refs 数组。
type extraRefs struct {
	Refs []AttachmentRef `json:"refs"`
}

// refMatchJSON 构造 GIN @> 查询用的 JSON 片段：{"refs":[{"kind":...,"id":...}]}。
// jsonb 数组包含语义是「元素级包含」，对象比较为键子集匹配，
// 因此 {"kind","id"} 即可命中带额外 title 键的引用项。
func refMatchJSON(kind string, refID string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"refs": []AttachmentRef{{Kind: kind, ID: refID}},
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// ListRefs 读取附件的引用缓存（无 extra_info / 无 refs / 非对象时返回空）。
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
		// 历史非对象/非 JSON 形态：视为无引用，不阻断引用保护主流程。
		return nil, nil
	}
	return parsed.Refs, nil
}

// HasRef 判断附件是否已被指定引用方引用。
func (m *AttachmentModel) HasRef(ctx context.Context, id uint64, kind string, refID string) (bool, error) {
	match, err := refMatchJSON(kind, refID)
	if err != nil {
		return false, err
	}
	var n int64
	// 对象包含（jsonb @>）而非数组表达式：走 GIN(extra_info) 索引，
	// 语义为「refs 数组里存在该 kind+id 的引用项」（对象比较按键子集）。
	if err := m.attrDB(ctx).Where("id = ? AND extra_info @> ?::jsonb", id, match).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// AddRef 追加一条引用（同 kind+id 已存在时不重复追加），返回本次是否真的新增。
//
// 判重与追加必须在**同一条 SQL** 里完成：原先「HasRef 查一次 → UPDATE 追加」是
// check-then-act，两个页面并发构建且引用同一张图时双方都查到 false，各自追加
// → refs 出现重复项（summarizeRefs 计数虚高），与本文件头「全部更新走单条 SQL」
// 的声明不符。改成 WHERE NOT (extra_info @> ?) 守卫后，并发的第二次 UPDATE
// 匹配 0 行，由 RowsAffected 判定「本次是否新增」（行锁天然把两次 UPDATE 串行化）。
// extra_info 为 SQL NULL 时 `@>` 返回 NULL、`NOT NULL` 仍为 NULL，故必须显式放行 IS NULL。
func (m *AttachmentModel) AddRef(ctx context.Context, id uint64, ref AttachmentRef) (bool, error) {
	match, err := refMatchJSON(ref.Kind, ref.ID)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(ref)
	if err != nil {
		return false, err
	}
	// 根节点必须是对象：extra_info 为 SQL NULL / jsonb null / 数组 / 标量时统一归零为 {}。
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

// RemoveRef 移除指定 kind+id 的引用（不存在时为 no-op），返回是否移除。
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

// ReplaceRefs 全量替换附件的 refs 数组（构建期「该引用方当前引用哪些附件」的落库动作）。
func (m *AttachmentModel) ReplaceRefs(ctx context.Context, id uint64, refs []AttachmentRef) error {
	if refs == nil {
		refs = []AttachmentRef{}
	}
	payload, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	// 同上：根节点非对象时归零为 {}，避免 jsonb_set 在标量/null 上报错。
	const setRefs = `jsonb_set(
        CASE WHEN jsonb_typeof(extra_info) = 'object' THEN extra_info ELSE '{}'::jsonb END,
        '{refs}', ?::jsonb, true)`
	return m.attrDB(ctx).Where("id = ?", id).Update("extra_info", gorm.Expr(setRefs, string(payload))).Error
}

// ListIDsByRef 反向查询「哪些附件被该引用方引用」（走 GIN 索引 idx_att_extra_info_gin）。
// 构建期全量替换时用于算差集：不再出现的附件需移除引用。
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
