package feature

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
)

func customerFeatureJSONForm(t *testing.T, form url.Values) string {
	t.Helper()
	body := map[string]any{}
	for key, values := range form {
		if key == "ids" {
			ids := make([]uint64, len(values))
			for i, v := range values {
				n, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				ids[i] = n
			}
			body[key] = ids
		} else if key == "customerId" || key == "toStatus" {
			n, err := strconv.Atoi(values[0])
			if err != nil {
				t.Fatal(err)
			}
			if key == "toStatus" {
				key = "status"
			}
			body[key] = n
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
