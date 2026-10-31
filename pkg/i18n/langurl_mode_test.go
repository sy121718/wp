package i18n

import "testing"

func TestParseSiteLangURLMode(t *testing.T) {
	cases := []struct {
		raw    string
		want   SiteLangURLMode
		wantOK bool
	}{
		{raw: "off", want: SiteLangURLModeOff, wantOK: true},
		{raw: "default_plain", want: SiteLangURLModeDefaultPlain, wantOK: true},
		{raw: "all_prefix", want: SiteLangURLModeAllPrefix, wantOK: true},

		{raw: "", want: SiteLangURLModeDefaultPlain, wantOK: true},
		{raw: "   ", want: SiteLangURLModeDefaultPlain, wantOK: true},

		{raw: "OFF", wantOK: false},
		{raw: "default", wantOK: false},
		{raw: "prefix", wantOK: false},
	}
	for _, c := range cases {
		got, err := ParseSiteLangURLMode(c.raw)
		if c.wantOK {
			if err != nil {
				t.Errorf("ParseSiteLangURLMode(%q) 不该报错: %v", c.raw, err)
				continue
			}
			if got != c.want {
				t.Errorf("ParseSiteLangURLMode(%q) = %q, 期望 %q", c.raw, got, c.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseSiteLangURLMode(%q) 应该拒绝非法值，实际返回 %q", c.raw, got)
		}
	}
}

func TestSiteLangURLModePurePredicates(t *testing.T) {
	cases := []struct {
		mode              SiteLangURLMode
		wantSeparated     bool
		wantPrefixDefault bool
	}{
		{SiteLangURLModeOff, false, false},
		{SiteLangURLModeDefaultPlain, true, false},
		{SiteLangURLModeAllPrefix, true, true},
	}
	for _, c := range cases {
		if got := SiteLangURLsSeparated(c.mode); got != c.wantSeparated {
			t.Errorf("SiteLangURLsSeparated(%q) = %v, 期望 %v", c.mode, got, c.wantSeparated)
		}
		if got := SiteLangURLPrefixDefault(c.mode); got != c.wantPrefixDefault {
			t.Errorf("SiteLangURLPrefixDefault(%q) = %v, 期望 %v", c.mode, got, c.wantPrefixDefault)
		}
	}
}
