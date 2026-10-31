package productdto

import (
	"math"
	"strconv"
	"strings"
)

type FieldKind string

const (
	FieldKindNumber FieldKind = "number"

	FieldKindRange FieldKind = "range"

	FieldKindJSON FieldKind = "json"
)

type FieldValueError struct {
	Field string

	Kind FieldKind
}

func (e *FieldValueError) Error() string {
	if e == nil {
		return ""
	}
	return "form field " + e.Field + ": " + string(e.Kind)
}

func FormText(raw string) string {
	return strings.TrimSpace(raw)
}

func OptFloat(field, raw string) (*float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || isNotFinite(v) {
		return nil, &FieldValueError{Field: field, Kind: FieldKindNumber}
	}
	return &v, nil
}

func OptInt(field, raw string) (*int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, &FieldValueError{Field: field, Kind: FieldKindNumber}
	}
	return &v, nil
}

func FormInt(field, raw string, def int) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return def, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def, &FieldValueError{Field: field, Kind: FieldKindNumber}
	}
	return v, nil
}

func isNotFinite(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0)
}
