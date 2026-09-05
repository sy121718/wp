package video

import "testing"

// TestEmbedHostAllowed 嵌入域名白名单：白名单域及其子域放行，
// 伪装后缀域（evilyoutube.com 等）拒绝（M 级修复回归）。
func TestEmbedHostAllowed(t *testing.T) {
	cases := []struct {
		name string
		host string
		want bool
	}{
		{"白名单主域", "youtube.com", true},
		{"白名单 www 子域", "www.youtube.com", true},
		{"白名单多级子域", "music.youtube.com", true},
		{"youtu.be 主域", "youtu.be", true},
		{"youtu.be www 子域", "www.youtu.be", true},
		{"vimeo.com 主域", "vimeo.com", true},
		{"vimeo.com 子域", "player.vimeo.com", true},
		{"player.vimeo.com 精确", "player.vimeo.com", true},
		// 绕过向量：裸 HasSuffix(host, "youtube.com") 会放行以下伪装域。
		{"伪装前缀 evilyoutube.com", "evilyoutube.com", false},
		{"伪装前缀 notvimeo.com", "notvimeo.com", false},
		{"中缀 evil-youtu.be", "evil-youtu.be", false},
		{"后缀拼接 youtube.com.evil.com", "youtube.com.evil.com", false},
		{"大写域名大小写不敏感放行", "WWW.YouTube.com", true},
		{"大写伪装域仍拒绝", "EVILYOUTUBE.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := embedHostAllowed(c.host); got != c.want {
				t.Errorf("embedHostAllowed(%q) = %v, 期望 %v", c.host, got, c.want)
			}
		})
	}
}

// TestEmbedURL embedURL 端到端：直通分支仅放行精确白名单域，
// 伪装后缀域在直通分支被拒绝（不被 youtubeRe/vimeoRe 匹配的 URL 形式）。
func TestEmbedURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantOK  bool
		wantURL string
	}{
		{
			name:    "youtube watch 正则重写",
			raw:     "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			wantOK:  true,
			wantURL: "https://www.youtube.com/embed/dQw4w9WgXcQ",
		},
		{
			name:    "youtube 短链正则重写",
			raw:     "https://youtu.be/dQw4w9WgXcQ",
			wantOK:  true,
			wantURL: "https://www.youtube.com/embed/dQw4w9WgXcQ",
		},
		{
			name:    "vimeo 正则重写",
			raw:     "https://vimeo.com/76979871",
			wantOK:  true,
			wantURL: "https://player.vimeo.com/video/76979871",
		},
		{
			name:    "直通：youtube playlist 非正则形式放行",
			raw:     "https://www.youtube.com/playlist?list=PL123",
			wantOK:  true,
			wantURL: "https://www.youtube.com/playlist?list=PL123",
		},
		{
			name:    "直通：player.vimeo.com 非数字路径放行",
			raw:     "https://player.vimeo.com/showcase/1234567",
			wantOK:  true,
			wantURL: "https://player.vimeo.com/showcase/1234567",
		},
		{
			name:   "直通拒绝：evilyoutube.com 伪装域（修复前 HasSuffix 绕过）",
			raw:    "https://evilyoutube.com/playlist?list=PL123",
			wantOK: false,
		},
		{
			name:   "直通拒绝：notvimeo.com 伪装域",
			raw:    "https://notvimeo.com/playlist",
			wantOK: false,
		},
		{
			name:   "直通拒绝：youtube.com.evil.com 后缀拼接",
			raw:    "https://youtube.com.evil.com/playlist",
			wantOK: false,
		},
		{
			name:   "非 https 一律拒绝",
			raw:    "http://www.youtube.com/playlist?list=PL123",
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := embedURL(c.raw)
			if ok != c.wantOK {
				t.Fatalf("embedURL(%q) ok = %v, 期望 %v（got=%q）", c.raw, ok, c.wantOK, got)
			}
			if c.wantOK && got != c.wantURL {
				t.Errorf("embedURL(%q) = %q, 期望 %q", c.raw, got, c.wantURL)
			}
		})
	}
}
