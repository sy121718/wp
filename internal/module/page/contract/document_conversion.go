package pagecontract

import json "go_wp/pkg/jsonx"

type DocumentConversion struct {
	Kind            string          `json:"kind"`
	ID              string          `json:"id"`
	ExpectedVersion int64           `json:"expectedVersion"`
	ExpectedHash    string          `json:"expectedHash"`
	Document        json.RawMessage `json:"document"`
}

type DocumentConversionRecord struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	BeforeVersion int64  `json:"beforeVersion"`
	AfterVersion  int64  `json:"afterVersion"`
	BeforeHash    string `json:"beforeHash"`
	AfterHash     string `json:"afterHash"`
}
