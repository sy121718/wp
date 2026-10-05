package aihttp

import (
	"net/http"
	"net/url"
	"strings"
)

// httptestNewForm 造一个 application/x-www-form-urlencoded 的 POST 请求。
//
// 不用 httptest.NewRequest 的字符串体：那样要把中文先 url-encode 一遍，
// 而测试里真正在读的是解码后的值 —— 手写编码会把「编码写错」混进被测对象里。
func httptestNewForm(fields map[string]string) *http.Request {
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	body := strings.NewReader(form.Encode())
	req, err := http.NewRequest(http.MethodPost, "/admin/ai/ask", body)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
