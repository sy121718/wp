package builder

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "go_wp/pkg/jsonx"
)

func DocumentDigest(raw []byte) (string, error) {
	normal, err := orderedDocumentJSON(raw)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(normal)
	return hex.EncodeToString(hash[:]), nil
}

func orderedDocumentJSON(raw []byte) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 {
		switch raw[0] {
		case '"':
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			return json.Marshal(value)
		case '{':
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil {
				return nil, err
			}
			for key, value := range object {
				ordered, err := orderedDocumentJSON(value)
				if err != nil {
					return nil, err
				}
				object[key] = ordered
			}
			return json.Marshal(object)
		case '[':
			var array []json.RawMessage
			if err := json.Unmarshal(raw, &array); err != nil {
				return nil, err
			}
			for i, value := range array {
				ordered, err := orderedDocumentJSON(value)
				if err != nil {
					return nil, err
				}
				array[i] = ordered
			}
			return json.Marshal(array)
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}
