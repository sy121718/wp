// Package main 可视化页面构建器独立启动入口。
//
// 只挂载 Jet 渲染器和 /builder 路由，不依赖数据库/Redis/认证等组件。
// 用于验证构建器功能能否走通。
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

var (
	pageIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	artifactMu    sync.Mutex
)

const (
	previewArtifactRoot = "public/builder-preview"
	maxGenerateBodySize = 4 << 20
)

type generateRequest struct {
	PageID   string          `json:"pageId"`
	HTML     string          `json:"html"`
	Document json.RawMessage `json:"document"`
}

type documentIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
}

func validateGenerateRequest(request generateRequest) error {
	if !pageIDPattern.MatchString(request.PageID) {
		return errors.New("页面 ID 格式无效")
	}
	if !strings.HasPrefix(strings.TrimSpace(request.HTML), "<!DOCTYPE html>") {
		return errors.New("HTML 产物格式无效")
	}
	if !json.Valid(request.Document) {
		return errors.New("Page Document 格式无效")
	}
	var identity documentIdentity
	if err := json.Unmarshal(request.Document, &identity); err != nil {
		return err
	}
	if identity.SchemaVersion != 2 || identity.ID != request.PageID {
		return errors.New("Page Document 与页面 ID 不匹配")
	}
	return nil
}

func writePreviewArtifact(root string, request generateRequest) error {
	if err := validateGenerateRequest(request); err != nil {
		return err
	}
	artifactMu.Lock()
	defer artifactMu.Unlock()

	directory := filepath.Join(root, request.PageID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
	}{
		{name: "index.html", data: []byte(request.HTML)},
		{name: "document.json", data: request.Document},
	}
	for _, file := range files {
		temporary := filepath.Join(directory, "."+file.name+".tmp")
		if err := os.WriteFile(temporary, file.data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(temporary, filepath.Join(directory, file.name)); err != nil {
			_ = os.Remove(temporary)
			return err
		}
	}
	return nil
}

func main() {
	gin.SetMode(gin.DebugMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Jet 模板渲染器（开发模式即时生效）
	router.HTMLRender = templates.NewJetHTMLRender("internal/templates", true)

	// 静态文件
	router.Static("/static", "internal/templates/static")
	if err := os.MkdirAll(previewArtifactRoot, 0o755); err != nil {
		log.Fatalf("创建测试产物目录失败: %v", err)
	}
	router.StaticFS("/builder-preview", http.Dir(previewArtifactRoot))

	// 测试构建只持久化静态产物和对应源码，不解释 Page Document。
	router.POST("/builder/generate", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxGenerateBodySize)
		var request generateRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "生成请求格式无效"})
			return
		}
		if err := writePreviewArtifact(previewArtifactRoot, request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
			return
		}
		url := "/builder-preview/" + request.PageID + "/"
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "测试 HTML 已生成", "data": gin.H{"url": url}})
	})

	router.GET("/builder/document", func(c *gin.Context) {
		pageID := c.Query("id")
		if !pageIDPattern.MatchString(pageID) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "页面 ID 格式无效"})
			return
		}
		path := filepath.Join(previewArtifactRoot, pageID, "document.json")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "Page Document 不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
			return
		}
		c.File(path)
	})

	// 构建器页面
	router.GET("/builder", func(c *gin.Context) {
		c.HTML(http.StatusOK, "builder/builder", gin.H{})
	})

	// 兜底重定向
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/builder")
	})

	addr := ":8080"
	log.Printf("构建器启动: http://localhost%s/builder", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}
