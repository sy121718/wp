package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// loadTestConfig 压测配置。
type loadTestConfig struct {
	baseURL          string
	listPath         string
	username         string
	password         string
	token            string
	signSecret       string
	page             int
	pageSize         int
	stageDurationSec int
	concurrencyList  []int
	timeoutSec       int
	minSuccessRate   float64
}

// loginResponse 登录接口响应结构。
type loginResponse struct {
	Code int `json:"code"`
	Data struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
	Msg string `json:"msg"`
}

// apiResponse 通用接口响应结构。
type apiResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// stageResult 单个并发档位结果。
type stageResult struct {
	concurrency int
	total       int64
	success     int64
	fail        int64
	elapsed     time.Duration
	totalQPS    float64
	successQPS  float64
	successRate float64
	p50         time.Duration
	p90         time.Duration
	p95         time.Duration
	p99         time.Duration
}

// requestResult 单次请求结果。
type requestResult struct {
	ok      bool
	latency time.Duration
}

func main() {
	cfg := parseFlags()

	httpClient := &http.Client{
		Timeout: time.Duration(cfg.timeoutSec) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        4096,
			MaxIdleConnsPerHost: 4096,
			MaxConnsPerHost:     0,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	accessToken := cfg.token
	if accessToken == "" {
		token, err := loginAndGetToken(httpClient, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
			os.Exit(1)
		}
		accessToken = token
		fmt.Println("登录成功，已拿到 access_token。")
	}

	fmt.Printf("开始压测: %s%s?page=%d&page_size=%d\n", cfg.baseURL, cfg.listPath, cfg.page, cfg.pageSize)
	fmt.Printf("每档持续: %ds, 并发档位: %v, 成功率阈值: %.2f%%\n", cfg.stageDurationSec, cfg.concurrencyList, cfg.minSuccessRate*100)
	fmt.Println(strings.Repeat("-", 120))
	fmt.Printf("%-8s %-10s %-10s %-10s %-10s %-10s %-10s %-10s %-10s %-10s\n",
		"并发", "总请求", "成功", "失败", "总QPS", "成功QPS", "成功率", "P50(ms)", "P95(ms)", "P99(ms)")
	fmt.Println(strings.Repeat("-", 120))

	results := make([]stageResult, 0, len(cfg.concurrencyList))
	for _, c := range cfg.concurrencyList {
		stageRes := runStage(httpClient, cfg, accessToken, c)
		results = append(results, stageRes)

		fmt.Printf("%-8d %-10d %-10d %-10d %-10.1f %-10.1f %-9.2f%% %-10d %-10d %-10d\n",
			stageRes.concurrency,
			stageRes.total,
			stageRes.success,
			stageRes.fail,
			stageRes.totalQPS,
			stageRes.successQPS,
			stageRes.successRate*100,
			stageRes.p50.Milliseconds(),
			stageRes.p95.Milliseconds(),
			stageRes.p99.Milliseconds(),
		)
	}

	fmt.Println(strings.Repeat("-", 120))
	best, ok := pickBest(results, cfg.minSuccessRate)
	if !ok {
		best = pickBestAny(results)
		fmt.Printf("未达到成功率阈值 %.2f%%，以下为全档位中成功QPS最高结果:\n", cfg.minSuccessRate*100)
	} else {
		fmt.Printf("达到成功率阈值 %.2f%% 的最高承载结果:\n", cfg.minSuccessRate*100)
	}
	fmt.Printf("并发=%d, 成功QPS=%.1f, 总QPS=%.1f, 成功率=%.2f%%, P95=%dms, P99=%dms\n",
		best.concurrency, best.successQPS, best.totalQPS, best.successRate*100, best.p95.Milliseconds(), best.p99.Milliseconds())
}

// parseFlags 解析命令行参数。
func parseFlags() loadTestConfig {
	baseURL := flag.String("base-url", "http://127.0.0.1:8081", "服务地址")
	listPath := flag.String("path", "/api/admin/product/bench", "压测接口路径")
	username := flag.String("username", getEnvOrDefault("ADMIN_USERNAME", "sky"), "管理员用户名")
	password := flag.String("password", getEnvOrDefault("ADMIN_PASSWORD", "123456"), "管理员密码")
	token := flag.String("token", os.Getenv("ADMIN_ACCESS_TOKEN"), "已存在的 access_token（传入后跳过登录）")
	signSecret := flag.String("sign-secret", getEnvOrDefault("SIGN_SECRET", "erp-sign-secret-key-2024-secure"), "签名密钥")
	page := flag.Int("page", 1, "列表页码")
	pageSize := flag.Int("page-size", 20, "分页大小")
	stageDurationSec := flag.Int("stage-sec", 10, "每档并发持续秒数")
	concurrencyCSV := flag.String("concurrency", "10,20,40,80,120,160,200", "并发档位，逗号分隔")
	timeoutSec := flag.Int("timeout-sec", 10, "单请求超时秒数")
	minSuccessRate := flag.Float64("min-success-rate", 0.99, "成功率阈值，范围 0~1")
	flag.Parse()

	concurrencyList, err := parseConcurrencyList(*concurrencyCSV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "并发档位参数错误: %v\n", err)
		os.Exit(1)
	}
	if *stageDurationSec <= 0 {
		fmt.Fprintln(os.Stderr, "stage-sec 必须大于 0")
		os.Exit(1)
	}
	if *timeoutSec <= 0 {
		fmt.Fprintln(os.Stderr, "timeout-sec 必须大于 0")
		os.Exit(1)
	}
	if *page <= 0 || *pageSize <= 0 {
		fmt.Fprintln(os.Stderr, "page/page-size 必须大于 0")
		os.Exit(1)
	}
	if *minSuccessRate < 0 || *minSuccessRate > 1 {
		fmt.Fprintln(os.Stderr, "min-success-rate 必须在 0 到 1 之间")
		os.Exit(1)
	}

	return loadTestConfig{
		baseURL:          strings.TrimRight(*baseURL, "/"),
		listPath:         *listPath,
		username:         *username,
		password:         *password,
		token:            *token,
		signSecret:       *signSecret,
		page:             *page,
		pageSize:         *pageSize,
		stageDurationSec: *stageDurationSec,
		concurrencyList:  concurrencyList,
		timeoutSec:       *timeoutSec,
		minSuccessRate:   *minSuccessRate,
	}
}

// parseConcurrencyList 解析并发档位参数。
func parseConcurrencyList(csv string) ([]int, error) {
	parts := strings.Split(csv, ",")
	if len(parts) == 0 {
		return nil, fmt.Errorf("并发档位不能为空")
	}

	list := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("非法并发值: %q", part)
		}
		list = append(list, v)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("并发档位不能为空")
	}
	sort.Ints(list)
	return list, nil
}

// loginAndGetToken 登录后台并获取 access_token。
func loginAndGetToken(httpClient *http.Client, cfg loadTestConfig) (string, error) {
	loginURL := cfg.baseURL + "/api/admin/login"
	payload := map[string]string{
		"username": cfg.username,
		"password": cfg.password,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化登录参数失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, loginURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("创建登录请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求登录接口失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("登录 HTTP 状态=%d, 响应=%s", resp.StatusCode, string(respBody))
	}

	var loginResp loginResponse
	if err := json.Unmarshal(respBody, &loginResp); err != nil {
		return "", fmt.Errorf("解析登录响应失败: %w, 原始响应=%s", err, string(respBody))
	}
	if loginResp.Code != 0 {
		return "", fmt.Errorf("登录业务失败 code=%d msg=%s", loginResp.Code, loginResp.Msg)
	}
	if loginResp.Data.AccessToken == "" {
		return "", fmt.Errorf("登录成功但 access_token 为空")
	}
	return loginResp.Data.AccessToken, nil
}

// runStage 执行单个并发档位压测。
func runStage(httpClient *http.Client, cfg loadTestConfig, token string, concurrency int) stageResult {
	var total int64
	var success int64
	var fail int64

	duration := time.Duration(cfg.stageDurationSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	resultsCh := make(chan requestResult, 2048)
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				reqRes := doListRequest(httpClient, cfg, token)
				resultsCh <- reqRes
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	latencies := make([]time.Duration, 0, 10000)
	for reqRes := range resultsCh {
		atomic.AddInt64(&total, 1)
		latencies = append(latencies, reqRes.latency)
		if reqRes.ok {
			atomic.AddInt64(&success, 1)
		} else {
			atomic.AddInt64(&fail, 1)
		}
	}

	elapsed := time.Since(start)
	totalCount := atomic.LoadInt64(&total)
	successCount := atomic.LoadInt64(&success)
	failCount := atomic.LoadInt64(&fail)

	totalQPS := float64(totalCount) / elapsed.Seconds()
	successQPS := float64(successCount) / elapsed.Seconds()
	successRate := 0.0
	if totalCount > 0 {
		successRate = float64(successCount) / float64(totalCount)
	}

	p50, p90, p95, p99 := calcPercentiles(latencies)

	return stageResult{
		concurrency: concurrency,
		total:       totalCount,
		success:     successCount,
		fail:        failCount,
		elapsed:     elapsed,
		totalQPS:    totalQPS,
		successQPS:  successQPS,
		successRate: successRate,
		p50:         p50,
		p90:         p90,
		p95:         p95,
		p99:         p99,
	}
}

// doListRequest 发起一次列表请求并返回结果。
func doListRequest(httpClient *http.Client, cfg loadTestConfig, token string) requestResult {
	begin := time.Now()
	params := map[string]string{
		"page":      strconv.Itoa(cfg.page),
		"page_size": strconv.Itoa(cfg.pageSize),
	}
	timestamp := time.Now().UnixMilli()
	nonce := generateNonce(16)
	signature := generateSign(params, timestamp, nonce, cfg.signSecret)

	query := url.Values{}
	query.Set("page", params["page"])
	query.Set("page_size", params["page_size"])

	reqURL := fmt.Sprintf("%s%s?%s", cfg.baseURL, cfg.listPath, query.Encode())
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return requestResult{ok: false, latency: time.Since(begin)}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Sign", signature)

	resp, err := httpClient.Do(req)
	if err != nil {
		return requestResult{ok: false, latency: time.Since(begin)}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	ok := false
	if resp.StatusCode == http.StatusOK {
		var respBody apiResponse
		if err := json.Unmarshal(bodyBytes, &respBody); err == nil && respBody.Code == 0 {
			ok = true
		}
	}

	return requestResult{ok: ok, latency: time.Since(begin)}
}

// generateSign 生成签名字符串。
func generateSign(params map[string]string, timestamp int64, nonce, secret string) string {
	allParams := make(map[string]string, len(params)+2)
	for key, value := range params {
		allParams[key] = value
	}
	allParams["timestamp"] = strconv.FormatInt(timestamp, 10)
	allParams["nonce"] = nonce

	keys := make([]string, 0, len(allParams))
	for key := range allParams {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for idx, key := range keys {
		if idx > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(key)
		sb.WriteByte('=')
		sb.WriteString(allParams[key])
	}

	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(sb.String()))
	return hex.EncodeToString(h.Sum(nil))
}

// generateNonce 生成随机 nonce。
func generateNonce(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if length <= 0 {
		length = 16
	}

	buf := make([]byte, length)
	max := big.NewInt(int64(len(chars)))
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			buf[i] = chars[time.Now().UnixNano()%int64(len(chars))]
			continue
		}
		buf[i] = chars[n.Int64()]
	}
	return string(buf)
}

// calcPercentiles 计算常用延迟分位值。
func calcPercentiles(latencies []time.Duration) (p50, p90, p95, p99 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0, 0
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	p50 = percentile(latencies, 0.50)
	p90 = percentile(latencies, 0.90)
	p95 = percentile(latencies, 0.95)
	p99 = percentile(latencies, 0.99)
	return
}

// percentile 从有序数组中取分位值。
func percentile(sortedLatencies []time.Duration, ratio float64) time.Duration {
	if len(sortedLatencies) == 0 {
		return 0
	}
	if ratio <= 0 {
		return sortedLatencies[0]
	}
	if ratio >= 1 {
		return sortedLatencies[len(sortedLatencies)-1]
	}

	idx := int(float64(len(sortedLatencies)-1) * ratio)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sortedLatencies) {
		idx = len(sortedLatencies) - 1
	}
	return sortedLatencies[idx]
}

// pickBest 从满足成功率阈值的结果中选择最高成功QPS。
func pickBest(results []stageResult, minSuccessRate float64) (stageResult, bool) {
	var best stageResult
	found := false
	for _, res := range results {
		if res.successRate < minSuccessRate {
			continue
		}
		if !found || res.successQPS > best.successQPS {
			best = res
			found = true
		}
	}
	return best, found
}

// pickBestAny 从所有结果中选择最高成功QPS。
func pickBestAny(results []stageResult) stageResult {
	if len(results) == 0 {
		return stageResult{}
	}
	best := results[0]
	for idx := 1; idx < len(results); idx++ {
		if results[idx].successQPS > best.successQPS {
			best = results[idx]
		}
	}
	return best
}

// getEnvOrDefault 获取环境变量，空则返回默认值。
func getEnvOrDefault(envKey, defaultVal string) string {
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return defaultVal
	}
	return value
}
