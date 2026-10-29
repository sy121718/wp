package builder

import (
	"context"
	"reflect"
	"testing"

	json "go_wp/pkg/jsonx"
)

func TestBusinessPresetCatalogAndDocumentsShareAuthority(t *testing.T) {
	catalog := BusinessPresets()
	if len(catalog) == 0 {
		t.Fatal("业务预设目录不能为空")
	}
	registry := BusinessPresetRegistry{}
	for _, meta := range catalog {
		document, err := registry.ResolvePreset(context.Background(), meta.PresetKey, meta.PresetVersion)
		if err != nil {
			t.Fatal(err)
		}
		page, err := ParsePage(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePage(page); err != nil {
			t.Fatal(err)
		}
		original := append([]byte(nil), document...)
		document[0] = '!'
		again, err := registry.ResolvePreset(context.Background(), meta.PresetKey, meta.PresetVersion)
		if err != nil || !reflect.DeepEqual([]byte(again), original) {
			t.Fatalf("预设读取必须隔离调用者修改: %v", err)
		}
		encoded, _ := json.Marshal(meta)
		var fields map[string]any
		_ = json.Unmarshal(encoded, &fields)
		if _, ok := fields["document"]; ok {
			t.Fatal("目录不得输出权威Document")
		}
	}
	catalog[0].PresetKey = "tampered"
	if BusinessPresets()[0].PresetKey == "tampered" {
		t.Fatal("目录必须是副本")
	}
	for _, test := range []struct {
		key     string
		version int
	}{{"unknown", 1}, {"commerce.cart-checkout", 0}, {"commerce.cart-checkout", 2}} {
		if _, err := registry.ResolvePreset(context.Background(), test.key, test.version); err == nil {
			t.Fatalf("未知预设版本不能回退: %+v", test)
		}
	}
}
