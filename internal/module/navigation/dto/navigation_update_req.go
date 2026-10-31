package navigationdto

import (
	"bytes"
	"errors"
	"net/url"
	"slices"
	"strconv"
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

type UpdateEnvelope struct {
	Action    string `json:"action" form:"action"`
	HasAction bool   `json:"-" form:"-"`

	Rows []TranslationWriteRow `json:"rows" form:"-"`

	LegacyBody []byte `json:"-" form:"-"`

	raw     map[string]json.RawMessage
	form    bool
	shapeOK bool

	formErr error
}

type TranslationWriteRow struct {
	Context string `json:"context" form:"context"`
	Hash    string `json:"hash" form:"hash"`
	Target  string `json:"target" form:"target"`
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
	return e.parse(body, false)
}

func (e *UpdateEnvelope) BindForm(values url.Values) error {
	e.Action = ""
	e.HasAction = false
	e.Rows = nil
	e.raw = nil
	e.form = false
	e.shapeOK = false
	e.formErr = nil
	if values == nil {
		e.formErr = ErrUpdateEnvelopeBody
		return e.formErr
	}

	if encoded := values.Encode(); encoded != "" {
		e.LegacyBody = []byte(encoded)
	}
	raw, err := updateFormRaw(values)
	if err != nil {
		e.formErr = err
		return err
	}
	e.applyRaw(raw, true)
	return e.formErr
}

func (e *UpdateEnvelope) parse(body []byte, form bool) error {
	e.Action = ""
	e.HasAction = false
	e.Rows = nil
	e.raw = nil
	e.form = false
	e.shapeOK = false
	e.formErr = nil
	raw, err := updateJSONRaw(body)
	if err != nil {
		e.formErr = err
		return err
	}
	e.applyRaw(raw, form)
	return e.formErr
}

func updateRowsCell(value json.RawMessage, form bool) json.RawMessage {
	if !form || value == nil {
		return value
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return value
	}
	return json.RawMessage(text)
}

func (e *UpdateEnvelope) applyRaw(raw map[string]json.RawMessage, form bool) {
	e.raw = raw
	e.form = form
	e.Action, e.HasAction, e.shapeOK = updateActionOf(raw[updateActionKey])

	switch e.Action {
	case updateActionTranslations:
		if !updateTranslationTopAllowed(raw) {
			e.formErr = ErrUpdateEnvelopeShape
			return
		}
	case updateActionMove:
		if !updateMoveTopAllowed(raw) {
			e.formErr = ErrUpdateEnvelopeShape
			return
		}
	}
	if e.Route() != RouteTranslations {
		return
	}

	rows, ok := updateTranslationRows(updateRowsCell(raw[updateRowsKey], form))
	if !ok {
		e.formErr = ErrUpdateEnvelopeShape
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
	if e.formErr != nil {
		return e.formErr
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
	payload, err := updateLegacyPayload(e.LegacyBody, e.form)
	if err != nil {
		return nil, err
	}
	req := &UpdateReq{}
	if err := ginbinding.JSON.BindBody(payload, req); err != nil {
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

func (e *UpdateEnvelope) IsForm() bool { return e.form }

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

func updateFormRaw(values url.Values) (map[string]json.RawMessage, error) {
	if updateIsEmptyQuery(values) {
		return nil, ErrUpdateEnvelopeBody
	}
	raw := make(map[string]json.RawMessage, len(values))
	for key, list := range values {
		if len(list) == 0 {

			raw[key] = json.RawMessage("null")
			continue
		}
		encoded, err := json.Marshal(list[len(list)-1])
		if err != nil {
			return nil, ErrUpdateEnvelopeBody
		}
		raw[key] = encoded
	}
	return raw, nil
}

func updateIsEmptyQuery(values url.Values) bool {
	for _, list := range values {
		for _, v := range list {
			if strings.TrimSpace(v) != "" {
				return false
			}
		}
	}
	return true
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

func updateLegacyPayload(body []byte, form bool) ([]byte, error) {
	if !form {
		return body, nil
	}
	raw, err := updateFormRawValues(body)
	if err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(raw))
	for key, list := range raw {
		if len(list) == 0 {
			continue
		}
		text := list[len(list)-1]
		if typed, ok := updateFormScalar(text); ok {
			out[key] = typed
			continue
		}
		encoded, err := json.Marshal(text)
		if err != nil {
			return nil, ErrUpdateEnvelopeShape
		}
		out[key] = encoded
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, ErrUpdateEnvelopeShape
	}
	return payload, nil
}

func updateFormRawValues(body []byte) (url.Values, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil || updateIsEmptyQuery(values) {
		return nil, ErrUpdateEnvelopeShape
	}
	return values, nil
}

func updateFormScalar(text string) (json.RawMessage, bool) {
	if text == "null" {
		return json.RawMessage("null"), true
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, false
	}
	if _, err := strconv.Atoi(trimmed); err != nil {
		return nil, false
	}
	return json.RawMessage(trimmed), true
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
