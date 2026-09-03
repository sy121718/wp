// Package main Go MVC 项目的启动入口。
//
// 整体启动流程：
//  1. config.Init("config.yaml")       — 读取 YAML 配置文件，初始化 Viper 实例
//  2. config.InitComponents()           — 驱动所有 pkg 组件初始化（数据库、日志、缓存等）
//  3. middleware.Setup(router)          — 挂载全局默认中间件链（Recovery、CORS、安全头等）
//  4. routers.SetupRoutes(router, ...)  — 注册业务路由（/livez、/readyz、/api/*）
//  5. srv.ListenAndServe()              — 启动 HTTP 服务
//  6. 信号等待（SIGINT/SIGTERM）        — 优雅关闭：先关 HTTP，再关组件
//
// 组件生命周期由 config 包统一编排：
//   - config.InitComponents()  按 critical 优先级依次初始化
//   - config.CloseComponents() 按逆序关闭，释放连接、定时器、后台 goroutine
package main

import (
	"context"
	"errors"
	"fmt"
	"go_wp/config"
	"go_wp/internal/middleware"
	"go_wp/internal/routers"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	migrations "go_wp/public/migrations"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

// main 是 Go 程序唯一入口，编译后直接运行。
// 所有错误上报到 run() 后统一由此处 log.Fatalf 终止进程。
func main() {
	if err := run(); err != nil {
		logger.Scene("init").Error(err, "服务启动失败")
		log.Fatal("服务启动失败")
	}
}

// run 是实际的主流程函数。
// 返回 error 即可让 main() 终止进程，组件自身不调用 os.Exit。
func run() error {
	// 1) 加载配置 + 初始化组件（数据库、JWT、日志、缓存、队列等）。
	serverCfg, err := loadAndPrepareRuntime("config.yaml")
	if err != nil {
		return err
	}

	// 1.5) 数据库结构迁移：按版本幂等执行建表语句（空库可重建，重复执行跳过）。
	if err := runMigrations(); err != nil {
		return err
	}

	// 2) 构建 HTTP 路由引擎。
	//    - middleware.Setup()  挂载 Recovery / CORS / 安全头等全局中间件
	//    - routers.SetupRoutes() 注册业务路由
	router := buildHTTPRouter(config.ValidateReady)

	// 3) 端口抢占处理。
	//    - debug / test 模式：自动释放被占用的端口（仅白名单进程）
	//    - release 模式：只提示端口占用和 kill 命令，不自动结束进程
	if err := utils.EnsurePortReadyWithStrategy(serverCfg.PortStrategy, serverCfg.Mode, serverCfg.Port); err != nil {
		return err
	}

	addr := fmt.Sprintf(":%d", serverCfg.Port)
	logger.Scene("init").With("addr", addr).Info("服务启动")

	// 4) 创建 http.Server 实例，配置超时参数。
	srv := buildHTTPServer(serverCfg, router)

	// 5) 在独立 goroutine 中启动 HTTP 服务。
	//    - 正常关闭时 ListenAndServe 返回 http.ErrServerClosed（不算错误）
	//    - 启动失败（如端口冲突）通过 channel 传回主协程
	serverErrCh := serveHTTPServer(srv)

	// 6) 监听系统退出信号（Ctrl+C / kill），同时等待服务错误。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-serverErrCh:
		// 服务启动或运行期间发生非预期错误。
		if err != nil {
			if closeErr := config.CloseComponents(); closeErr != nil {
				logger.Scene("init").Error(closeErr, "组件关闭失败")
			}
			return fmt.Errorf("HTTP 服务启动失败: %w", err)
		}
		return nil
	case <-quit:
		logger.Scene("init").Info("收到退出信号，开始关闭...")
	}

	// 7) 优雅关闭 HTTP 服务：给在途请求最多 5 秒完成。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Scene("init").Error(err, "HTTP Server 关闭失败")
	}

	// 8) 按逆序关闭所有已初始化的组件（数据库连接、Redis、队列、日志等）。
	if err := config.CloseComponents(); err != nil {
		logger.Scene("init").Error(err, "组件关闭失败")
	}

	logger.Scene("init").Info("服务已退出")
	return nil
}

// runMigrations 在数据库组件就绪后执行结构迁移。
func runMigrations() error {
	db, err := database.GetDB()
	if err != nil {
		return fmt.Errorf("迁移前置检查失败: %w", err)
	}
	return migrations.Run(db)
}

// loadAndPrepareRuntime 加载配置并初始化运行时组件。
//
// 执行顺序：
//
// 调用方需要确保 config.Init() 先于 config.InitComponents() 执行，
// 因为 InitComponents() 内部依赖 Viper 读取各组件配置。
func loadAndPrepareRuntime(configPath string) (config.ServerConfig, error) {
	//  1. config.Init() — 读 config.yaml，创建全局 Viper 实例
	if err := config.Init(configPath); err != nil {
		return config.ServerConfig{}, fmt.Errorf("配置加载失败: %w", err)
	}
	//  2. config.GetServer()     — 解析 server 段配置（端口、模式、超时等）
	serverCfg, err := config.GetServer()
	if err != nil {
		return config.ServerConfig{}, err
	}
	//  3. gin.SetMode()  — 设置 Gin 运行模式（debug/release/test）
	gin.SetMode(serverCfg.Mode)
	//  4. config.InitComponents()— 按 critical 优先顺序初始化所有 pkg 组件
	if err := config.InitComponents(); err != nil {
		return config.ServerConfig{}, fmt.Errorf("组件初始化失败: %w", err)
	}

	return serverCfg, nil
}

// buildHTTPRouter 创建 Gin 引擎并完成路由装配。
// ready 函数用于 /readyz 的健康检查，由 config.ValidateReady() 提供。
func buildHTTPRouter(
	ready func() error,
) *gin.Engine {
	router := gin.New()
	//  1. middleware.Setup()     — 挂载全局中间件（Recovery → CORS → 安全头 → BodyLimit → RateLimit → LogCapture）
	middleware.Setup(router)
	//  2. routers.SetupRoutes() — 注册健康检查路由（/livez、/readyz）和业务路由（/api/*）
	routers.SetupRoutes(router, ready)
	return router
}

// buildHTTPServer 创建标准 http.Server 实例。
//
// 超时参数说明：
//   - ReadHeaderTimeout：读取请求头的超时，防止慢连接攻击
//   - ReadTimeout：读取完整请求体的超时
//   - WriteTimeout：写入响应的超时
//   - IdleTimeout：Keep-Alive 空闲超时，超过后关闭连接
//
// 这些超时从 config.yaml server 段读取，由 config.GetServer() 解析。
func buildHTTPServer(serverCfg config.ServerConfig, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", serverCfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: serverCfg.ReadHeaderTimeout,
		ReadTimeout:       serverCfg.ReadTimeout,
		WriteTimeout:      serverCfg.WriteTimeout,
		IdleTimeout:       serverCfg.IdleTimeout,
	}
}

// serveHTTPServer 在独立 goroutine 中启动 HTTP 服务。
//
// 返回一个带缓冲的 error channel，调用方通过 select 等待：
//   - 服务正常关闭（ListenAndServe 返回 nil 或 http.ErrServerClosed）→ channel 不会收到错误
//   - 服务启动失败（如端口被占用）→ channel 收到非 nil 错误
//
// channel 在函数返回前被关闭，确保 select 不会永远阻塞。
func serveHTTPServer(srv *http.Server) chan error {
	serverErrCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
		close(serverErrCh)
	}()
	return serverErrCh
}
