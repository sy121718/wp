package uploadprovider

import "testing"

// Windows 保留设备名（CON/PRN/AUX/NUL/COM1-9/LPT1-9，含带扩展名与大小写变体）应前缀转义。
func TestSanitizeFilenameWindowsReservedNames(t *testing.T) {
	cases := []struct{ in, want string }{
		{"CON", "_CON"},
		{"con.txt", "_con.txt"},
		{"NUL", "_NUL"},
		{"COM1", "_COM1"},
		{"com9.log", "_com9.log"},
		{"LPT5", "_LPT5"},
		{"AUX", "_AUX"},
		{"PRN", "_PRN"},
		{"CON.txt", "_CON.txt"},
		// 非保留名：长度 5 的 COM10/LPT10 不是设备名，不应误伤。
		{"COM10", "COM10"},
		{"LPT10", "LPT10"},
		// 普通文件名不受影响。
		{"report.txt", "report.txt"},
		{"report", "report"},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 尾随点/空格（Windows 与对象存储会静默裁剪，造成保留名命中或扩展名伪装）应去掉。
func TestSanitizeFilenameTrailingDotsAndSpaces(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report.txt. ", "report.txt"},
		{"name. ", "name"},
		{"name .", "name"},
		{"CON.txt. ", "_CON.txt"},
		{"  spaced  ", "spaced"},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 路径分隔符与危险字符应替换为下划线；"."/".."/空串应返回空。
func TestSanitizeFilenamePathCharsAndTraversal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/", "_"},
		{"a/b", "a_b"},
		{`a\b`, "a_b"},
		{"a:b", "a_b"},
		{"a*b", "a_b"},
		{"a?b", "a_b"},
		{`a"b`, "a_b"},
		{"a<b", "a_b"},
		{"a>b", "a_b"},
		{"a|b", "a_b"},
		{"..", ""},
		{".", ""},
		{"", ""},
		{"  ", ""},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
