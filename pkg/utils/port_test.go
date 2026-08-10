package utils

import "testing"

func TestResolvePortStrategy(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		mode     string
		want     string
	}{
		{name: "explicit auto release", strategy: "auto_release", mode: "release", want: "auto_release"},
		{name: "explicit report only", strategy: "report_only", mode: "debug", want: "report_only"},
		{name: "debug fallback", strategy: "", mode: "debug", want: "auto_release"},
		{name: "test fallback", strategy: "", mode: "test", want: "auto_release"},
		{name: "release fallback", strategy: "", mode: "release", want: "report_only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePortStrategy(tt.strategy, tt.mode)
			if got != tt.want {
				t.Fatalf("端口策略不正确: got=%s want=%s", got, tt.want)
			}
		})
	}
}
