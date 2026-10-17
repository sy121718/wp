package jsonx_test

import (
	"bytes"
	v1 "encoding/json"
	"errors"
	"go_wp/pkg/jsonx"
	"reflect"
	"strings"
	"testing"
	"time"
)

type inner struct {
	A string `json:"a,omitempty"`
}

type sample struct {
	Str    string           `json:"str"`
	Empty  string           `json:"empty,omitempty"`
	NilSl  []string         `json:"nilSl"`
	EmpSl  []string         `json:"empSl"`
	NilMp  map[string]int   `json:"nilMp"`
	EmpMp  map[string]int   `json:"empMp"`
	N      int              `json:"n"`
	T      time.Time        `json:"t"`
	HT     string           `json:"ht"`
	Raw    jsonx.RawMessage `json:"raw"`
	Pt     *inner           `json:"pt"`
	In     inner            `json:"in"`
	F      float64          `json:"f"`
	Num    jsonx.Number     `json:"num"`
	Nums   []jsonx.Number   `json:"nums"`
	U      uint64           `json:"u"`
	B      []byte           `json:"b"`
	Any    any              `json:"any"`
	MapAny map[string]any   `json:"mapAny"`
	Strs   []string         `json:"strs"`
}

func base() sample {
	return sample{
		Str:    "x",
		EmpSl:  []string{},
		EmpMp:  map[string]int{},
		T:      time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
		HT:     "<a>&\"b\"</a>",
		Raw:    jsonx.RawMessage("{\"k\":1}"),
		In:     inner{},
		F:      1e21,
		Num:    jsonx.Number("1.500"),
		Nums:   []jsonx.Number{"2", "3.25", "-7"},
		U:      18446744073709551615,
		B:      []byte{0xff, 0x00, 0x41},
		Any:    map[string]any{"z": 1, "a": "s", "m": nil},
		MapAny: map[string]any{"k9": 9.0, "k1": 1.0, "k5": 5.0, "k3": 3.0, "k2": 2.0, "k8": 8.0},
		Strs:   []string{"a", "", "字"},
	}
}

// TestMarshalByteIdenticalWithV1 是本包存在的理由：默认选项下 jsonx 的输出必须与
// encoding/json 逐字节一致。任何一条还原选项被删/改错，这里立刻红。
func TestMarshalByteIdenticalWithV1(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"零值", sample{}},
		{"基准", base()},
		{"nil切片与nil映射", sample{NilSl: nil, NilMp: nil, EmpSl: []string{}, EmpMp: map[string]int{}}},
		{"HTML字符", sample{HT: "<script>alert(1)</script>&\u2028"}},
		{"数字边界", sample{N: -1, U: 0, F: 1e-7}},
		{"map键顺序", map[string]int{"b": 2, "a": 1, "c": 3, "z": 26, "m": 13}},
		{"嵌套指针", sample{Pt: &inner{A: ""}, In: inner{A: "v"}}},
		{"RawMessage空", sample{Raw: jsonx.RawMessage("null")}},
		{"裸切片", []string{"x", "", "y"}},
		{"裸map", map[string]any{"n": 1.5, "s": "t", "b": true, "nul": nil}},
		{"时间是零值", sample{T: time.Time{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, e1 := v1.Marshal(tc.val)
			got, e2 := jsonx.Marshal(tc.val)
			if (e1 == nil) != (e2 == nil) {
				t.Fatalf("错误不一致: v1=%v jsonx=%v", e1, e2)
			}
			if string(want) != string(got) {
				t.Fatalf("字节不一致\n v1  = %s\n jsonx= %s", want, got)
			}
		})
	}
}

// TestUnmarshalMatchesV1 解析侧同等：宽容度（重复键、大小写字段名、null、未知字段）与结果必须一致。
func TestUnmarshalMatchesV1(t *testing.T) {
	inputs := []string{
		`{"str":"y","nilSl":null,"nilMp":null,"empSl":[],"empMp":{},"n":1}`,
		`{"STR":"case-insensitive"}`,
		`{"a":1,"a":2,"str":"dup"}`,
		`{"unknown":1,"str":"v"}`,
		`{"num":"1.5","nums":["2","3"]}`,
		`{"t":"2026-10-16T12:00:00Z","f":1e21}`,
		`{"b":"//4A","raw":{"z":1},"pt":{"a":"q"}}`,
		`{}`,
	}
	for i, in := range inputs {
		var a, b sample
		e1 := v1.Unmarshal([]byte(in), &a)
		e2 := jsonx.Unmarshal([]byte(in), &b)
		if (e1 == nil) != (e2 == nil) {
			t.Fatalf("case %d %s: 错误不一致 v1=%v jsonx=%v", i, in, e1, e2)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("case %d %s:\n v1   = %#v\n jsonx = %#v", i, in, a, b)
		}
	}
}

// TestStrictUnknownMember 钉住「需要 v2 独有能力时怎么用」：按错误值分流，不按文案。
func TestStrictUnknownMember(t *testing.T) {
	var s sample
	err := jsonx.Unmarshal([]byte(`{"unknownField":1}`), &s, jsonx.RejectUnknownMembers(true))
	if err == nil {
		t.Fatal("RejectUnknownMembers 应拒绝未知字段")
	}
	if !errors.Is(err, jsonx.ErrUnknownName) {
		t.Fatalf("应能用 errors.Is 分流未知字段，得到 %v", err)
	}
	if strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("v2 文案不该含 v1 的 unknown field：%v", err)
	}
	var ok sample
	if e := jsonx.Unmarshal([]byte(`{"unknownField":1}`), &ok); e != nil {
		t.Fatalf("默认应忽略未知字段: %v", e)
	}
}

// TestHelpersMatchV1 辅助函数的等价性。
func TestHelpersMatchV1(t *testing.T) {
	for _, in := range []string{`{"a":1}`, `{oops`, ``, `[]`, `nul`, `123`, `"s"`} {
		if jsonx.Valid([]byte(in)) != v1.Valid([]byte(in)) {
			t.Fatalf("Valid 不一致: %q", in)
		}
	}
	src := []byte("{ \"a\" : 1, \"b\" : [1,2] }")
	var c1, c2 bytes.Buffer
	if err := v1.Compact(&c1, src); err != nil {
		t.Fatal(err)
	}
	if err := jsonx.Compact(&c2, src); err != nil {
		t.Fatal(err)
	}
	if c1.String() != c2.String() {
		t.Fatalf("Compact 不一致: %q vs %q", c1.String(), c2.String())
	}
	var i1, i2 bytes.Buffer
	if err := v1.Indent(&i1, src, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := jsonx.Indent(&i2, src, "", "  "); err != nil {
		t.Fatal(err)
	}
	if i1.String() != i2.String() {
		t.Fatalf("Indent 不一致:\n%s\n---\n%s", i1.String(), i2.String())
	}
	m1, _ := v1.MarshalIndent(map[string]int{"a": 1, "b": 2}, "", "  ")
	m2, _ := jsonx.MarshalIndent(map[string]int{"a": 1, "b": 2}, "", "  ")
	if string(m1) != string(m2) {
		t.Fatalf("MarshalIndent 不一致:\n%s\n---\n%s", m1, m2)
	}
	var wb bytes.Buffer
	if err := jsonx.MarshalWrite(&wb, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if wb.String() != `{"a":1}` {
		t.Fatalf("MarshalWrite: %s", wb.String())
	}
}
