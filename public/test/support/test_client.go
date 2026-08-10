package support

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

type RequestOptions struct {
	Method  string
	Path    string
	Headers map[string]string
	Query   map[string]string
	Body    interface{}
	RawBody []byte
}

type StandardResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func SendRequest(engine *gin.Engine, options RequestOptions) (*httptest.ResponseRecorder, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine 不能为空")
	}

	path := strings.TrimSpace(options.Path)
	if path == "" {
		return nil, fmt.Errorf("path 不能为空")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	method := strings.TrimSpace(options.Method)
	if method == "" {
		method = http.MethodGet
	}

	requestPath, err := buildRequestPath(path, options.Query)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := buildRequestBody(options.Body, options.RawBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, requestPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	for key, value := range options.Headers {
		req.Header.Set(key, value)
	}

	if len(bodyBytes) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder, nil
}

func ParseStandardResponse(recorder *httptest.ResponseRecorder) (*StandardResponse, error) {
	if recorder == nil {
		return nil, fmt.Errorf("recorder 不能为空")
	}

	var result StandardResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("解析标准响应失败: %w", err)
	}
	return &result, nil
}

func DecodeResponseBody[T any](recorder *httptest.ResponseRecorder, out *T) error {
	if recorder == nil {
		return fmt.Errorf("recorder 不能为空")
	}
	if out == nil {
		return fmt.Errorf("out 不能为空")
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
		return fmt.Errorf("解析响应体失败: %w", err)
	}
	return nil
}

func DecodeResponseData[T any](recorder *httptest.ResponseRecorder, out *T) error {
	if out == nil {
		return fmt.Errorf("out 不能为空")
	}

	result, err := ParseStandardResponse(recorder)
	if err != nil {
		return err
	}

	if len(result.Data) == 0 || string(result.Data) == "null" {
		return fmt.Errorf("响应中没有 data 字段")
	}

	if err := json.Unmarshal(result.Data, out); err != nil {
		return fmt.Errorf("解析 data 字段失败: %w", err)
	}
	return nil
}

func buildRequestPath(path string, query map[string]string) (string, error) {
	parsedURL, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("解析 path 失败: %w", err)
	}

	values := parsedURL.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	parsedURL.RawQuery = values.Encode()
	return parsedURL.String(), nil
}

func buildRequestBody(body interface{}, rawBody []byte) ([]byte, error) {
	if body != nil && len(rawBody) > 0 {
		return nil, fmt.Errorf("body 和 rawBody 不能同时设置")
	}

	if len(rawBody) > 0 {
		return rawBody, nil
	}

	if body == nil {
		return nil, nil
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化 body 失败: %w", err)
	}
	return bodyBytes, nil
}
