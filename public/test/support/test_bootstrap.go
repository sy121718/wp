package support

import (
	"fmt"
	"go_wp/config"
	"go_wp/internal/middleware"
	"go_wp/internal/routers"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// BootstrapOptions 测试初始化选项。
type BootstrapOptions struct {
	ConfigPath      string
	GinMode         string
	InitComponents  bool
	UseDefaultRoute bool
	RouteRegistrar  func(engine *gin.Engine)
}

// SetupTestBootstrap 初始化测试用 gin 引擎并返回清理函数。
func SetupTestBootstrap(options BootstrapOptions) (*gin.Engine, func() error, error) {
	config.ResetForTest()

	configPath := strings.TrimSpace(options.ConfigPath)
	if configPath == "" {
		configPath = "config.yaml"
	}
	configPath = resolveConfigPath(configPath)

	if err := config.Init(configPath); err != nil {
		return nil, nil, fmt.Errorf("初始化配置失败: %w", err)
	}

	ginMode := strings.TrimSpace(options.GinMode)
	if ginMode == "" {
		ginMode = gin.TestMode
	}
	gin.SetMode(ginMode)

	componentsInited := false
	if options.InitComponents {
		if err := config.InitComponents(); err != nil {
			return nil, nil, fmt.Errorf("初始化组件失败: %w", err)
		}
		componentsInited = true
	}

	engine := gin.New()
	middleware.Setup(engine)

	useDefaultRoute := options.UseDefaultRoute
	if !useDefaultRoute && options.RouteRegistrar == nil {
		useDefaultRoute = true
	}

	if useDefaultRoute {
		routers.SetupRoutes(engine, config.ValidateReady)
	}

	if options.RouteRegistrar != nil {
		options.RouteRegistrar(engine)
	}

	var once sync.Once
	cleanup := func() error {
		var closeErr error
		once.Do(func() {
			if componentsInited {
				closeErr = config.CloseComponents()
			}
			config.ResetForTest()
		})
		return closeErr
	}

	return engine, cleanup, nil
}

func resolveConfigPath(configPath string) string {
	if filepath.IsAbs(configPath) {
		return configPath
	}

	if fileExists(configPath) {
		return configPath
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return configPath
	}

	currentDir := workingDir
	for {
		candidate := filepath.Join(currentDir, configPath)
		if fileExists(candidate) {
			return candidate
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}
		currentDir = parentDir
	}

	return configPath
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
