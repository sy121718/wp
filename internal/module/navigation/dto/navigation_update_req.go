package navigationdto

import (
	"bytes"
	"errors"
	"slices"
	"strings"

	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
)

type RouteKind int

const (
	RouteLegacy RouteKind = iota

	RouteTranslations

	RouteMove

	RouteRejected
)

const (
	updateActionKey = "action"
	updateRowsKey   = "rows"

	updateActionTranslations = "translations"
	updateActionMove         = "move"

	updateTranslationProjectKey = "project"
	updateTranslationLangKey    = "lang"

	updateCSRFKey = "csrf_token"
)

var updateReservedKeys = []string{
	updateRowsKey, updateTranslationProjectKey, updateTranslationLangKey, "dir", "projectId",
}

var updateTranslationTopKeys = []string{
	updateActionKey, updateTranslationProjectKey, updateTranslationLangKey, updateRowsKey, updateCSRFKey,
}

var updateMoveTopKeys = []string{
	updateActionKey, "id", "projectId", "dir",
}

var ErrUpdateEnvelopeShape = errors.New("navigationdto: update envelope shape rejected")

var ErrUpdateEnvelopeBody = errors.New("navigationdto: update envelope body unusable")

// UpdateEnvelope 是 /api/navigation/translations 的 JSON 输入封套。
//
// 它只解 JSON：表单写入走 /api/navigation/update（UpdateReq），两条路由各有
// 一份确定协议，不存在「同一端点两种编码」的形状。
type UpdateEnvelope struct {
	Action    string `json:"action"`
	HasAction bool   `json:"-"`

	// Project / Lang 只在 translations 分支出现，值来自 raw 里已白名单的顶层键。
	// 显式声明成字段（而不是只留在 raw 里）是为了让线上形状对契约生成器可见：
	// 写基座提交的就是 action/project/lang/rows 四个顶层键。
	Project string `json:"project"`
	Lang    string `json:"lang"`

	Rows []TranslationWriteRow `json:"rows"`

	LegacyBody []byte `json:"-"`

	raw     map[string]json.RawMessage
	shapeOK bool

	parseErr error
}

type TranslationWriteRow struct {
	Context string `json:"context"`
	Hash    string `json:"hash"`
	Target  string `json:"target"`
}

type TranslationUpdateReq struct {
	ProjectID string
	Lang      string
	Rows      []TranslationWriteRow
}

type MoveUpdateReq struct {
	ID        string
	ProjectID string
	Dir       string
}

func (e *UpdateEnvelope) UnmarshalJSON(body []byte) error {
	e.LegacyBody = body
	return e.parse(body)
}

func (e *UpdateEnvelope) parse(body []byte) error {
	e.Action = ""
	e.HasAction = false
	e.Project = ""
	e.Lang = ""
	e.Rows = nil
	e.raw = nil
	e.shapeOK = false
	e.parseErr = nil
	raw, err := updateJSONRaw(body)
	if err != nil {
		e.parseErr = err
		return err
	}
	e.applyRaw(raw)
	return e.parseErr
}

func (e *UpdateEnvelope) applyRaw(raw map[string]json.RawMessage) {
	e.raw = raw
	e.Action, e.HasAction, e.shapeOK = updateActionOf(raw[updateActionKey])

	switch e.Action {
	case updateActionTranslations:
		if !updateTranslationTopAllowed(raw) {
			e.parseErr = ErrUpdateEnvelopeShape
			return
		}
	case updateActionMove:
		if !updateMoveTopAllowed(raw) {
			e.parseErr = ErrUpdateEnvelopeShape
			return
		}
	}
	if e.Route() != RouteTranslations {
		return
	}
	// 顶层键已经过白名单校验，这里把它们落到显式字段上，供契约与调用方读取。
	e.Project, _ = updateRequiredString(raw[updateTranslationProjectKey])
	e.Lang, _ = updateRequiredString(raw[updateTranslationLangKey])

	rows, ok := updateTranslationRows(raw[updateRowsKey])
	if !ok {
		e.parseErr = ErrUpdateEnvelopeShape
		return
	}
	e.Rows = rows
}

func (e *UpdateEnvelope) Route() RouteKind {
	if e.HasAction {
		if !e.shapeOK {
			return RouteRejected
		}
		switch e.Action {
		case updateActionTranslations:
			return RouteTranslations
		case updateActionMove:
			return RouteMove
		default:
			return RouteRejected
		}
	}
	for _, key := range updateReservedKeys {
		if _, ok := e.raw[key]; ok {
			return RouteRejected
		}
	}
	return RouteLegacy
}

func (e *UpdateEnvelope) DecodeTranslation() (*TranslationUpdateReq, error) {
	if e.Route() != RouteTranslations {
		return nil, ErrUpdateEnvelopeShape
	}
	projectID, ok := updateRequiredString(e.raw[updateTranslationProjectKey])
	if !ok {
		return nil, ErrUpdateEnvelopeShape
	}
	lang, ok := updateRequiredString(e.raw[updateTranslationLangKey])
	if !ok || !updateLangSupported(lang) {
		return nil, ErrUpdateEnvelopeShape
	}
	return &TranslationUpdateReq{ProjectID: projectID, Lang: lang, Rows: e.Rows}, nil
}

func (e *UpdateEnvelope) Validate() error {
	if e == nil {
		return ErrUpdateEnvelopeBody
	}
	if e.parseErr != nil {
		return e.parseErr
	}
	if e.Route() != RouteLegacy {
		return nil
	}
	if _, err := e.DecodeLegacy(); err != nil {
		return err
	}
	return nil
}

func (e *UpdateEnvelope) ValidateMove() error {
	_, err := e.MoveRequest()
	return err
}

func (e *UpdateEnvelope) DecodeLegacy() (*UpdateReq, error) {
	if e.Route() != RouteLegacy {
		return nil, ErrUpdateEnvelopeShape
	}
	req := &UpdateReq{}
	if err := ginbinding.JSON.BindBody(e.LegacyBody, req); err != nil {
		return nil, ErrUpdateEnvelopeShape
	}
	return req, nil
}

func (e *UpdateEnvelope) MoveRequest() (*MoveUpdateReq, error) {
	if e.Route() != RouteMove {
		return nil, ErrUpdateEnvelopeShape
	}
	id, ok := updateRequiredString(e.raw["id"])
	if !ok {
		return nil, ErrUpdateEnvelopeShape
	}
	dir, ok := updateRequiredString(e.raw["dir"])
	if !ok {
		return nil, ErrUpdateEnvelopeShape
	}
	req := &MoveUpdateReq{ID: id, Dir: dir}

	if value, present := e.raw["projectId"]; present {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrUpdateEnvelopeShape
		}
		text, ok := updateOptionalString(value)
		if !ok {
			return nil, ErrUpdateEnvelopeShape
		}
		req.ProjectID = text
	}
	return req, nil
}

func updateJSONRaw(body []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, ErrUpdateEnvelopeBody
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return nil, ErrUpdateEnvelopeBody
	}
	return raw, nil
}

func updateTranslationTopAllowed(raw map[string]json.RawMessage) bool {
	return updateTopAllowed(raw, updateTranslationTopKeys)
}

func updateMoveTopAllowed(raw map[string]json.RawMessage) bool {
	return updateTopAllowed(raw, updateMoveTopKeys)
}

func updateTopAllowed(raw map[string]json.RawMessage, allowed []string) bool {
	for key := range raw {
		if !slices.Contains(allowed, key) {
			return false
		}
	}
	return true
}

func updateActionOf(value json.RawMessage) (string, bool, bool) {
	if value == nil {
		return "", false, true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", true, false
	}
	var action string
	if err := json.Unmarshal(value, &action); err != nil {
		return "", true, false
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return "", true, false
	}
	return action, true, true
}

func updateRequiredString(value json.RawMessage) (string, bool) {
	text, ok := updateOptionalString(value)
	if !ok || text == "" {
		return "", false
	}
	return text, true
}

func updateOptionalString(value json.RawMessage) (string, bool) {
	if value == nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", true
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func updateLangSupported(lang string) bool {
	return slices.Contains(i18n.AvailableLangs(), lang)
}

var updateTranslationRowKeys = []string{"context", "hash", "target"}

func updateTranslationRows(value json.RawMessage) ([]TranslationWriteRow, bool) {
	if value == nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, false
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(value, &list); err != nil {
		return nil, false
	}
	if len(list) > updateTranslationMaxRows {
		return nil, false
	}

	out := make([]TranslationWriteRow, 0, len(list))
	for _, item := range list {
		for _, required := range updateTranslationRowKeys {
			if _, ok := item[required]; !ok {
				return nil, false
			}
		}
		row := TranslationWriteRow{}
		for key, cell := range item {
			if !slices.Contains(updateTranslationRowKeys, key) {
				return nil, false
			}
			text, ok := updateRowCellText(cell)
			if !ok {
				return nil, false
			}
			switch key {
			case "context":
				row.Context = text
			case "hash":
				row.Hash = text
			case "target":
				row.Target = text
			}
		}
		out = append(out, row)
	}
	return out, true
}

func updateRowCellText(cell json.RawMessage) (string, bool) {
	if bytes.Equal(bytes.TrimSpace(cell), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(cell, &text); err != nil {
		return "", false
	}
	return text, true
}

const updateTranslationMaxRows = 1000
