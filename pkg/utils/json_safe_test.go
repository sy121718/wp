package utils

import (
	json "go_wp/pkg/jsonx"
	"testing"
)

func TestJsonSafe(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "ordinary", input: `{"name":"alpha"}`, want: `{"name":"alpha"}`},
		{name: "script close", input: `{"html":"</script><script>"}`, want: `{"html":"<\/script><script>"}`},
		{name: "case preserved", input: `{"html":"</ScRiPt></SCRIPT>"}`, want: `{"html":"<\/ScRiPt><\/SCRIPT>"}`},
		{name: "html comment", input: `{"html":"<!-- x -->"}`, want: `{"html":"<\!-- x -->"}`},
		{name: "adjacent boundaries", input: `{"html":"</<!--</"}`, want: `{"html":"<\/<\!--<\/"}`},
		{name: "existing escape untouched", input: `{"html":"<\/script>"}`, want: `{"html":"<\/script>"}`},
		{name: "empty", input: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := JsonSafe(tc.input); got != tc.want {
				t.Errorf("JsonSafe(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// The legacy comment escape is intentionally preserved, including its decode error.
func TestJsonSafeRawCommentDecodeError(t *testing.T) {
	if err := json.Unmarshal([]byte(JsonSafe(`{"html":"<!--"}`)), new(any)); err == nil {
		t.Fatal("raw comment escape must preserve the legacy JSON decode error")
	}
}

func TestJsonSafeJSONDecodeEquivalent(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "script and comment", input: `{"html":"</ScRiPt>\u003c!-- note --\u003e","nested":["</",1,true,null]}`},
		{name: "unicode and escaped slash", input: `{"html":"\u4f60\u597d <\/script> \u003c!-- \u003c"}`},
		{name: "plain object", input: `{"items":[],"ok":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var before, after any
			if err := json.Unmarshal([]byte(tc.input), &before); err != nil {
				t.Fatalf("invalid test input: %v", err)
			}
			if err := json.Unmarshal([]byte(JsonSafe(tc.input)), &after); err != nil {
				t.Fatalf("escaped JSON became invalid: %v", err)
			}
			original, _ := json.Marshal(before)
			escaped, _ := json.Marshal(after)
			if string(original) != string(escaped) {
				t.Errorf("decoded values differ: %s != %s", original, escaped)
			}
		})
	}
}
