package uploadprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

const (
	qiniuName              string = "qiniu"
	defaultQiniuUploadHost string = "https://upload.qiniup.com"
)

type qiniuProvider struct {
	mu     sync.RWMutex
	inited bool
	client *http.Client
}

func NewQiniuProvider() Provider {
	return &qiniuProvider{}
}

func (p *qiniuProvider) Name() string {
	return qiniuName
}

func (p *qiniuProvider) Init(_ *viper.Viper) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.inited {
		return nil
	}

	p.client = &http.Client{
		Timeout: 60 * time.Second,
	}
	p.inited = true
	return nil
}

func (p *qiniuProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.inited = false
	p.client = nil
	return nil
}

func (p *qiniuProvider) Upload(ctx context.Context, cfg RuntimeConfig, file File, req Request) (Result, error) {
	if file.Reader == nil {
		return Result{}, fmt.Errorf("上传文件为空")
	}
	if strings.TrimSpace(file.Filename) == "" && strings.TrimSpace(req.ObjectKey) == "" {
		return Result{}, fmt.Errorf("上传文件名缺失")
	}

	client, err := p.getClient()
	if err != nil {
		return Result{}, err
	}

	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	bucket := strings.TrimSpace(cfg.Bucket)
	if accessKey == "" || secretKey == "" || bucket == "" {
		return Result{}, fmt.Errorf("七牛配置缺少 access_key/secret_key/bucket")
	}

	objectKey, err := buildObjectKey(file.Filename, req)
	if err != nil {
		return Result{}, err
	}

	uploadHost := resolveQiniuUploadHost(cfg)
	token, err := buildQiniuUploadToken(accessKey, secretKey, bucket, objectKey)
	if err != nil {
		return Result{}, fmt.Errorf("生成上传 token 失败: %w", err)
	}

	bodyReader, contentType, sizeCounter, err := buildQiniuMultipartBody(file, objectKey, token)
	if err != nil {
		return Result{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadHost, bodyReader)
	if err != nil {
		return Result{}, fmt.Errorf("创建七牛上传请求失败: %w", err)
	}
	request.Header.Set("Content-Type", contentType)

	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("请求七牛上传失败: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Result{}, fmt.Errorf("读取七牛响应失败: %w", err)
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, fmt.Errorf("七牛上传失败: status=%d, body=%s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	publicURL := buildQiniuPublicURL(cfg, objectKey)
	return Result{
		Provider: qiniuName,
		Key:      objectKey,
		URL:      publicURL,
		Size:     sizeCounter.Size(),
	}, nil
}

func (p *qiniuProvider) getClient() (*http.Client, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.inited || p.client == nil {
		return nil, fmt.Errorf("上传组件未初始化")
	}
	return p.client, nil
}

func buildQiniuUploadToken(accessKey string, secretKey string, bucket string, objectKey string) (string, error) {
	putPolicy := map[string]any{
		"scope":    bucket + ":" + objectKey,
		"deadline": time.Now().Unix() + 3600,
	}

	policyBytes, err := json.Marshal(putPolicy)
	if err != nil {
		return "", fmt.Errorf("序列化七牛上传策略失败: %w", err)
	}

	encodedPolicy := base64.RawURLEncoding.EncodeToString(policyBytes)
	mac := hmac.New(sha1.New, []byte(secretKey))
	if _, err := mac.Write([]byte(encodedPolicy)); err != nil {
		return "", fmt.Errorf("生成七牛上传签名失败: %w", err)
	}

	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return strings.TrimSpace(accessKey) + ":" + signature + ":" + encodedPolicy, nil
}

func resolveQiniuUploadHost(cfg RuntimeConfig) string {
	if cfg.Extra != nil {
		if host := strings.TrimSpace(cfg.Extra["upload_host"]); host != "" {
			return strings.TrimRight(host, "/")
		}
		if host := strings.TrimSpace(cfg.Extra["endpoint"]); host != "" {
			return strings.TrimRight(host, "/")
		}
	}

	if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}

	region := strings.ToLower(strings.TrimSpace(cfg.Region))
	switch region {
	case "z0", "huadong", "east", "cn-east-1":
		return "https://upload.qiniup.com"
	case "z1", "huabei", "north", "cn-north-1":
		return "https://upload-z1.qiniup.com"
	case "z2", "huanan", "south", "cn-south-1":
		return "https://upload-z2.qiniup.com"
	case "na0", "beimei", "north-america":
		return "https://upload-na0.qiniup.com"
	case "as0", "dongnanya", "southeast-asia":
		return "https://upload-as0.qiniup.com"
	default:
		return defaultQiniuUploadHost
	}
}

func buildQiniuPublicURL(cfg RuntimeConfig, objectKey string) string {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" && cfg.Extra != nil {
		baseURL = strings.TrimSpace(cfg.Extra["base_url"])
	}

	key := strings.TrimLeft(strings.ReplaceAll(objectKey, "\\", "/"), "/")
	if baseURL == "" {
		return key
	}
	return strings.TrimRight(baseURL, "/") + "/" + key
}

func buildQiniuMultipartBody(file File, objectKey string, token string) (io.Reader, string, *writeCounter, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	counter := &writeCounter{}

	go func() {
		defer pw.Close()

		if err := writer.WriteField("token", token); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("写入 token 失败: %w", err))
			return
		}
		if err := writer.WriteField("key", objectKey); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("写入 key 失败: %w", err))
			return
		}

		fileName := file.Filename
		if strings.TrimSpace(fileName) == "" {
			fileName = objectKey
		}

		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			_ = pw.CloseWithError(fmt.Errorf("创建 multipart 文件字段失败: %w", err))
			return
		}

		if _, err := io.Copy(io.MultiWriter(part, counter), file.Reader); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("写入文件内容失败: %w", err))
			return
		}

		if err := writer.Close(); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("关闭 multipart writer 失败: %w", err))
			return
		}
	}()

	return pr, writer.FormDataContentType(), counter, nil
}

type writeCounter struct {
	mu   sync.Mutex
	size int64
}

func (w *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	w.mu.Lock()
	w.size += int64(n)
	w.mu.Unlock()
	return n, nil
}

func (w *writeCounter) Size() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.size
}
