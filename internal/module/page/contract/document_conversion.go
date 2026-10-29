package pagecontract

import json "go_wp/pkg/jsonx"

// DocumentConversion 是已 dry-run 的有限替换，不解释转换规则或修改访问路径。
type DocumentConversion struct {
	Kind            string          `json:"kind"`
	ID              string          `json:"id"`
	ExpectedVersion int64           `json:"expectedVersion"`
	ExpectedHash    string          `json:"expectedHash"`
	Document        json.RawMessage `json:"document"`
}

// DocumentConversionRecord 与既有不可变 revision/version 对应，用于复核与重放。
type DocumentConversionRecord struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	BeforeVersion int64  `json:"beforeVersion"`
	AfterVersion  int64  `json:"afterVersion"`
	BeforeHash    string `json:"beforeHash"`
	AfterHash     string `json:"afterHash"`
}
