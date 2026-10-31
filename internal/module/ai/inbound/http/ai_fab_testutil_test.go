package aihttp

import (
	"net/http"
	"net/url"
	"strings"
)

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
