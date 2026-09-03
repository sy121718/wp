package text

import (
	"testing"
)

// TestSanitizeRichHTMLImg 验证 img 白名单：合法 img 保留、危险协议/事件属性剥离。
func TestSanitizeRichHTMLImg(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "合法 img 完整属性保留",
			src:  `<p><img src="https://example.com/a.jpg" alt="说明" width="300" height="200" loading="lazy"></p>`,
			want: `<p><img src="https://example.com/a.jpg" alt="说明" width="300" height="200" loading="lazy"></p>`,
		},
		{
			name: "自闭合 img 输出为 void 元素",
			src:  `<img src="/img/x.png" alt="" />`,
			want: `<img src="/img/x.png" alt="">`,
		},
		{
			name: "javascript src 被拒",
			src:  `<img src="javascript:alert(1)" alt="x">`,
			want: `<img alt="x">`,
		},
		{
			name: "data src 被拒",
			src:  `<img src="data:image/png;base64,xxx">`,
			want: `<img>`,
		},
		{
			name: "事件属性 onerror/onclick 被剥离",
			src:  `<img src="https://e.com/a.jpg" onerror="alert(1)" onclick="x()">`,
			want: `<img src="https://e.com/a.jpg">`,
		},
		{
			name: "非法 width 被拒",
			src:  `<img src="https://e.com/a.jpg" width="abc" height="200px">`,
			want: `<img src="https://e.com/a.jpg" height="200px">`,
		},
		{
			name: "figure/figcaption 包裹保留",
			src:  `<figure><img src="https://e.com/a.jpg"><figcaption>图片说明</figcaption></figure>`,
			want: `<figure><img src="https://e.com/a.jpg"><figcaption>图片说明</figcaption></figure>`,
		},
		{
			name: "异常闭合 </img> 被忽略",
			src:  `<img src="https://e.com/a.jpg"></img>`,
			want: `<img src="https://e.com/a.jpg">`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
		})
	}
}
