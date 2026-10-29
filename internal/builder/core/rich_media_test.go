package core

import (
	"errors"
	"strings"
	"testing"
)

func TestRichMediaRequiresReliableSource(t *testing.T) {
	for _, src := range []string{"/storage/483.jpg", "images/483.jpg", "/images/483.jpg", "483.jpg", ""} {
		_, err := NormalizeRichMediaURLs(`<p>正文<img src="` + src + `"></p>`)
		if !errors.Is(err, ErrIncompleteNode) || !strings.Contains(err.Error(), "图片 1") {
			t.Errorf("缺少可信基址或来源的图片需可定位错误: %q %v", src, err)
		}
	}
	for _, src := range []string{"ftp://example.test/image.jpg", "/storage/../private.jpg", "https://user:pass@example.test/a.jpg"} {
		_, err := NormalizeRichMediaURLs(`<img src="` + src + `">`)
		if err == nil || errors.Is(err, ErrIncompleteNode) {
			t.Errorf("非法媒体配置不能作为未完成容错: %q %v", src, err)
		}
	}
}

func TestRichMediaExternalAndSanitizerBoundary(t *testing.T) {
	src := `<p><img src='https://external.test/storage/a.jpg?x=1&amp;y=2' alt='外链'></p>`
	got, err := NormalizeRichMediaURLs(src)
	if err != nil || got != src {
		t.Fatalf("外链与无需改写的内容必须保真: %s %v", got, err)
	}
	got, err = NormalizeRichMediaURLs(`<img src="//cdn.example.test/a.jpg">`)
	if err != nil || got != `<img src="https://cdn.example.test/a.jpg">` {
		t.Fatalf("协议相对外链需完整HTTPS: %s %v", got, err)
	}
	if again, err := NormalizeRichMediaURLs(got); err != nil || again != got {
		t.Fatalf("媒体归一必须幂等: %s %v", again, err)
	}
	dangerous := `<img src="java&#x0A;script:alert(1)" onerror="x()" srcset="/storage/a.jpg 2x"><video poster="/storage/p.jpg"><source src="/storage/v.mp4"></video>`
	got, err = NormalizeRichMediaURLs(dangerous)
	if err != nil {
		t.Fatal(err)
	}
	cleaned := SanitizeRichHTML(got)
	for _, forbidden := range []string{"javascript", "script:", "onerror", "srcset", "video", "source", "poster", "/storage/"} {
		if strings.Contains(cleaned, forbidden) {
			t.Fatalf("不能扩展白名单或恢复危险属性 %s: %s", forbidden, cleaned)
		}
	}
}
