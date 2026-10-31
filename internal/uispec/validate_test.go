package uispec

import (
	"errors"
	"strings"
	"testing"
)

func mustReject(t *testing.T, raw, wantKind string) {
	t.Helper()
	_, err := Parse([]byte(raw))
	if err == nil {
		t.Fatalf("应被拒绝但通过了: %s", raw)
	}
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("错误没有归口到 ErrRejected（上层就不知道该降级）: %v", err)
	}
	var r *Reject
	if !errors.As(err, &r) {
		t.Fatalf("错误不是 *Reject，拿不到原因标签: %v", err)
	}
	if r.Kind != wantKind {
		t.Fatalf("原因标签不对: got %q want %q (%v)", r.Kind, wantKind, err)
	}
}

func TestParseAcceptsRenderableSpec(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"纯文字回答", `{"text":"这周卖得最好的是手工皂。"}`},
		{"文字 + 表格", `{"text":"见下表","blocks":[{"type":"table","source":"orders_top_products","params":{"from":"2026-01-01","to":"2026-01-07"},"limit":10}]}`},
		{"指标卡", `{"blocks":[{"type":"stat","source":"orders_daily"}]}`},
		{"列表与手风琴", `{"blocks":[{"type":"list","source":"orders_status_counts"},{"type":"accordion","source":"orders_daily"}]}`},
		{"blocks 显式为 null", `{"text":"hi","blocks":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Parse([]byte(tc.raw))
			if err != nil {
				t.Fatalf("应通过但被拒绝: %v", err)
			}
			if s == nil {
				t.Fatalf("返回了 nil spec")
			}
		})
	}
}

func TestParseRejectsUnknownType(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"chart","source":"orders_daily"}]}`, KindUnknownType)
	mustReject(t, `{"blocks":[{"type":"Table","source":"orders_daily"}]}`, KindUnknownType)
	mustReject(t, `{"blocks":[{"type":"","source":"orders_daily"}]}`, KindUnknownType)
}

func TestParseRejectsUnknownField(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"table","source":"x","rows":[["a","b"]]}]}`, KindUnknownField)
	mustReject(t, `{"blocks":[{"type":"stat","source":"x","value":"999"}]}`, KindUnknownField)
	mustReject(t, `{"text":"hi","actions":[{"kind":"filter"}]}`, KindUnknownField)
	mustReject(t, `{"text":"hi","blocks":[],"foo":1}`, KindUnknownField)
}

func TestParseAcceptsFieldNameCaseVariants(t *testing.T) {
	s, err := Parse([]byte(`{"blocks":[{"Type":"table","Source":"orders_daily"}]}`))
	if err != nil {
		t.Fatalf("encoding/json 的大小写不敏感匹配应让它通过: %v", err)
	}
	if len(s.Blocks) != 1 || s.Blocks[0].Type != TypeTable || s.Blocks[0].Source != "orders_daily" {
		t.Fatalf("字段没有落到预期位置: %+v", s.Blocks)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	mustReject(t, ``, KindMalformed)
	mustReject(t, `   `, KindMalformed)
	mustReject(t, `{"blocks":`, KindMalformed)
	mustReject(t, `{"text":"a"}{"text":"b"}`, KindMalformed)
	mustReject(t, `{"text":"a"} trailing`, KindMalformed)
}

func TestParseRejectsEmpty(t *testing.T) {
	mustReject(t, `{}`, KindEmpty)
	mustReject(t, `{"text":"   "}`, KindEmpty)
	mustReject(t, `{"blocks":[]}`, KindEmpty)
}

func TestParseRejectsMissingSource(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"table"}]}`, KindMissingSource)
	mustReject(t, `{"blocks":[{"type":"table","source":"  "}]}`, KindMissingSource)
}

func TestParseRejectsBadSourceName(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"table","source":"Orders"}]}`, KindBadValue)
	mustReject(t, `{"blocks":[{"type":"table","source":"orders-top"}]}`, KindBadValue)
	mustReject(t, `{"blocks":[{"type":"table","source":"9orders"}]}`, KindBadValue)
	mustReject(t, `{"blocks":[{"type":"table","source":"orders;drop"}]}`, KindBadValue)
}

func TestParseRejectsTooLarge(t *testing.T) {
	blocks := make([]string, 0, MaxBlocks+1)
	for i := 0; i <= MaxBlocks; i++ {
		blocks = append(blocks, `{"type":"list","source":"orders_daily"}`)
	}
	mustReject(t, `{"blocks":[`+strings.Join(blocks, ",")+`]}`, KindTooLarge)

	mustReject(t, `{"blocks":[{"type":"table","source":"x","title":"`+strings.Repeat("字", MaxTitleRunes+1)+`"}]}`, KindTooLarge)
	mustReject(t, `{"blocks":[{"type":"table","source":"x","params":{"q":"`+strings.Repeat("a", MaxParamValue+1)+`"}}]}`, KindTooLarge)

	params := make([]string, 0, MaxParams+1)
	for i := 0; i <= MaxParams; i++ {
		params = append(params, `"k`+string(rune('a'+i))+`":"v"`)
	}
	mustReject(t, `{"blocks":[{"type":"table","source":"x","params":{`+strings.Join(params, ",")+`}}]}`, KindTooLarge)

	mustReject(t, `{"blocks":[{"type":"table","source":"`+strings.Repeat("a", MaxSourceLen+1)+`"}]}`, KindTooLarge)
}

func TestParseRejectsBadLimit(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"table","source":"x","limit":`+itoa(MaxLimit+1)+`}]}`, KindBadValue)
	mustReject(t, `{"blocks":[{"type":"table","source":"x","limit":-1}]}`, KindBadValue)

	if _, err := Parse([]byte(`{"blocks":[{"type":"table","source":"x","limit":` + itoa(MaxLimit) + `}]}`)); err != nil {
		t.Fatalf("上限值本身应通过: %v", err)
	}
	if _, err := Parse([]byte(`{"blocks":[{"type":"table","source":"x"}]}`)); err != nil {
		t.Fatalf("省略 limit 应通过: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
