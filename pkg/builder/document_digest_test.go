package builder

import "testing"

func TestDocumentDigestMatchesJSONBFormattingWithoutNumberLoss(t *testing.T) {
	a, err := DocumentDigest([]byte(`{"z":[{"v":9007199254740993}],"a":1,"text":"\u003c"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := DocumentDigest([]byte(`{ "text":"<", "a": 1, "z": [ { "v": 9007199254740993 } ] }`))
	if err != nil || a != b {
		t.Fatalf("JSONB 排序与空白不能造成冲突: %s %s %v", a, b, err)
	}
	c, err := DocumentDigest([]byte(`{"a":1,"z":[{"v":9007199254740992}],"text":"<"}`))
	if err != nil || a == c {
		t.Fatalf("整数不能经 float64 丢失: %s %s %v", a, c, err)
	}
}
