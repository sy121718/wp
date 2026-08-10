package builtin

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"go_wp/pkg/crypto"

	"github.com/gin-gonic/gin"
)

func TestSignatureMiddlewareRejectsReplayNonce(t *testing.T) {
	ResetNonceStore()

	engine := gin.New()
	engine.GET("/signed", SignatureMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": true})
	})

	timestamp := time.Now().Unix()
	nonce := "nonce-123"
	params := map[string]interface{}{
		"foo":   "bar",
		"nonce": nonce,
	}
	signature := crypto.GenerateSignature(params, timestamp)

	firstRequest := httptest.NewRequest(
		http.MethodGet,
		"/signed?foo=bar&nonce="+nonce+"&timestamp="+strconv.FormatInt(timestamp, 10)+"&signature="+signature,
		nil,
	)
	firstRecorder := httptest.NewRecorder()
	engine.ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("首次请求状态码不正确: got=%d want=%d", firstRecorder.Code, http.StatusOK)
	}

	secondRequest := httptest.NewRequest(
		http.MethodGet,
		"/signed?foo=bar&nonce="+nonce+"&timestamp="+strconv.FormatInt(timestamp, 10)+"&signature="+signature,
		nil,
	)
	secondRecorder := httptest.NewRecorder()
	engine.ServeHTTP(secondRecorder, secondRequest)

	if secondRecorder.Body.String() == firstRecorder.Body.String() {
		t.Fatalf("重复请求不应继续命中业务处理器")
	}
	if secondRecorder.Code != http.StatusBadRequest {
		t.Fatalf("重复请求响应状态码不正确: got=%d want=%d", secondRecorder.Code, http.StatusBadRequest)
	}
	if !contains(secondRecorder.Body.String(), "\"code\":400") {
		t.Fatalf("重复请求应返回 400, got=%s", secondRecorder.Body.String())
	}
}

func contains(text string, target string) bool {
	return len(text) >= len(target) && (text == target || len(text) > len(target) && stringContains(text, target))
}

func stringContains(text string, target string) bool {
	for i := 0; i+len(target) <= len(text); i++ {
		if text[i:i+len(target)] == target {
			return true
		}
	}
	return false
}
