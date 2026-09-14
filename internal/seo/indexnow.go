package seo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go_wp/pkg/logger"
)

const indexNowEndpoint = "https://api.indexnow.org/indexnow"

// NotifyIndexNowAsync 异步 ping IndexNow；key 或 baseURL 为空时不做任何事。
// 失败只记日志，不影响发布主链。
func NotifyIndexNowAsync(baseURL, key string, paths []string) {
	key = strings.TrimSpace(key)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if key == "" || baseURL == "" || len(paths) == 0 {
		return
	}
	host, err := hostOfBaseURL(baseURL)
	if err != nil || host == "" {
		logger.Scene("seo").With("baseURL", baseURL).Warn("IndexNow 跳过：站点根地址无效")
		return
	}
	urls := make([]string, 0, len(paths))
	for _, p := range paths {
		p = CanonicalPublicPath(p)
		if p == "" {
			continue
		}
		urls = append(urls, JoinURL(baseURL, p))
	}
	if len(urls) == 0 {
		return
	}
	go pingIndexNow(host, key, baseURL, urls)
}

func pingIndexNow(host, key, baseURL string, urls []string) {
	body, err := json.Marshal(map[string]any{
		"host":        host,
		"key":         key,
		"keyLocation": JoinURL(baseURL, "/"+key+".txt"),
		"urlList":     urls,
	})
	if err != nil {
		logger.Scene("seo").Error(err, "IndexNow 请求体序列化失败")
		return
	}
	req, err := http.NewRequest(http.MethodPost, indexNowEndpoint, bytes.NewReader(body))
	if err != nil {
		logger.Scene("seo").Error(err, "IndexNow 请求构造失败")
		return
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Scene("seo").With("count", len(urls)).Error(err, "IndexNow ping 失败")
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Scene("seo").With("status", resp.StatusCode).With("count", len(urls)).
			Warn("IndexNow ping 非 2xx 响应")
	}
}

func hostOfBaseURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}
