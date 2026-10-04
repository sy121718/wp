package uispec

import (
	"errors"
	"strings"
	"testing"
)

// mustReject 断言一条 spec 被拒绝，且原因标签正确。
//
// 同时钉住两件事：① 归口（errors.Is(err, ErrRejected) —— 上层降级只认它）；
// ② 具体原因（Kind 进诊断事件）。只断言「报错了」会漏掉「报错但给不出原因」这种半成品。
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

// TestParseAcceptsRenderableSpec 正常形态必须通过 —— 拒绝太好写，容易把整个协议拒死。
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

// TestParseRejectsUnknownType 未知组件类型必须拒绝（闸门的一半）。
func TestParseRejectsUnknownType(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"chart","source":"orders_daily"}]}`, KindUnknownType)
	mustReject(t, `{"blocks":[{"type":"Table","source":"orders_daily"}]}`, KindUnknownType) // 大小写敏感
	mustReject(t, `{"blocks":[{"type":"","source":"orders_daily"}]}`, KindUnknownType)
}

// TestParseRejectsUnknownField 白名单之外的字段必须拒绝（闸门的另一半）。
//
// 最有代表性的是**块里偷带数据**：模型把行直接写进 spec，数字就绕过了「必须服务端查」。
// 放过去等于把这个协议的核心约束变成一句注释。
func TestParseRejectsUnknownField(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"table","source":"x","rows":[["a","b"]]}]}`, KindUnknownField)
	mustReject(t, `{"blocks":[{"type":"stat","source":"x","value":"999"}]}`, KindUnknownField)
	mustReject(t, `{"text":"hi","actions":[{"kind":"filter"}]}`, KindUnknownField)
	mustReject(t, `{"text":"hi","blocks":[],"foo":1}`, KindUnknownField)
}

// TestParseAcceptsFieldNameCaseVariants 记录一个**实测事实**，不是设计选择。
//
// encoding/json 匹配字段名时大小写不敏感（文档明写），所以 `{"Type":"table"}` 会被正确
// 解到 Type 字段上、而不是被 DisallowUnknownFields 拒掉。原本以为「大小写不同 = 未知字段」，
// 写用例时才发现不是 —— 留在这里免得下次又按想象写一遍。
//
// 不为此写自定义解码器：值完全一样，严格化的收益（协议只有一种写法）抵不上多一层解析代码
// 与它自己的出错面。真要收紧时，改这里的同时把上面那条用例一起改。
func TestParseAcceptsFieldNameCaseVariants(t *testing.T) {
	s, err := Parse([]byte(`{"blocks":[{"Type":"table","Source":"orders_daily"}]}`))
	if err != nil {
		t.Fatalf("encoding/json 的大小写不敏感匹配应让它通过: %v", err)
	}
	if len(s.Blocks) != 1 || s.Blocks[0].Type != TypeTable || s.Blocks[0].Source != "orders_daily" {
		t.Fatalf("字段没有落到预期位置: %+v", s.Blocks)
	}
}

// TestParseRejectsMalformed 语法层：空输入、非法 JSON、尾随内容。
func TestParseRejectsMalformed(t *testing.T) {
	mustReject(t, ``, KindMalformed)
	mustReject(t, `   `, KindMalformed)
	mustReject(t, `{"blocks":`, KindMalformed)
	mustReject(t, `{"text":"a"}{"text":"b"}`, KindMalformed) // 两个对象拼在一起
	mustReject(t, `{"text":"a"} trailing`, KindMalformed)
}

// TestParseRejectsEmpty 既没有文字也没有组件 = 无效回答。
func TestParseRejectsEmpty(t *testing.T) {
	mustReject(t, `{}`, KindEmpty)
	mustReject(t, `{"text":"   "}`, KindEmpty)
	mustReject(t, `{"blocks":[]}`, KindEmpty)
}

// TestParseRejectsMissingSource 块必须有数据源（没有 source 的块渲染出来是空壳）。
func TestParseRejectsMissingSource(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"table"}]}`, KindMissingSource)
	mustReject(t, `{"blocks":[{"type":"table","source":"  "}]}`, KindMissingSource)
}

// TestParseRejectsBadSourceName 数据源名的字符集约定。
func TestParseRejectsBadSourceName(t *testing.T) {
	mustReject(t, `{"blocks":[{"type":"table","source":"Orders"}]}`, KindBadValue)
	mustReject(t, `{"blocks":[{"type":"table","source":"orders-top"}]}`, KindBadValue)
	mustReject(t, `{"blocks":[{"type":"table","source":"9orders"}]}`, KindBadValue)
	mustReject(t, `{"blocks":[{"type":"table","source":"orders;drop"}]}`, KindBadValue)
}

// TestParseRejectsTooLarge 规模上限：块数、标题、参数值、行数上限。
func TestParseRejectsTooLarge(t *testing.T) {
	blocks := make([]string, 0, MaxBlocks+1)
	for i := 0; i <= MaxBlocks; i++ {
		blocks = append(blocks, `{"type":"list","source":"orders_daily"}`)
	}
	mustReject(t, `{"blocks":[`+strings.Join(blocks, ",")+`]}`, KindTooLarge)

	mustReject(t, `{"blocks":[{"type":"table","source":"x","title":"`+strings.Repeat("字", MaxTitleRunes+1)+`"}]}`, KindTooLarge)
	mustReject(t, `{"blocks":[{"type":"table","source":"x","params":{"q":"`+strings.Repeat("a", MaxParamValue+1)+`"}}]}`, KindTooLarge)

	// 参数条数
	params := make([]string, 0, MaxParams+1)
	for i := 0; i <= MaxParams; i++ {
		params = append(params, `"k`+string(rune('a'+i))+`":"v"`)
	}
	mustReject(t, `{"blocks":[{"type":"table","source":"x","params":{`+strings.Join(params, ",")+`}}]}`, KindTooLarge)

	// 数据源名超长
	mustReject(t, `{"blocks":[{"type":"table","source":"`+strings.Repeat("a", MaxSourceLen+1)+`"}]}`, KindTooLarge)
}

// TestParseRejectsBadLimit limit 越界（0 表示「用默认」，不算越界）。
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

// itoa 避免为一处转换引入 strconv（测试里只有正数）。
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
