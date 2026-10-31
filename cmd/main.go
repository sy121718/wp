package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"go_wp/config"
	"go_wp/internal/middleware"
	"go_wp/internal/partition"
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

func main() {

	migrateOnly := flag.Bool("migrate-only", false, "只执行数据库结构迁移与业务 seed，然后退出（不启动 HTTP 服务）")
	flag.Parse()

	if *migrateOnly {
		config.ForceMigrations()

		config.SuppressWorkers()
	}

	if err := run(*migrateOnly); err != nil {

		log.Fatalf("服务启动失败: %v", err)
	}
}

func run(migrateOnly bool) error {

	serverCfg, err := loadAndPrepareRuntime("config.yaml")
	if err != nil {
		return err
	}

	if migrateOnly {
		if config.RunMigrationsEnabled() {
			if err := runSeeds(); err != nil {
				return err
			}
			if err := runPartitionMaintenance(); err != nil {
				return err
			}
		}
		logger.Scene("init").Info("数据库迁移与 seed 完成（-migrate-only），未启动 HTTP 服务")
		if closeErr := config.CloseComponents(); closeErr != nil {
			logger.Scene("init").Error(closeErr, "组件关闭失败")
		}
		return nil
	}

	router := buildHTTPRouter(config.ValidateReady)

	if err := utils.EnsurePortReadyWithStrategy(serverCfg.PortStrategy, serverCfg.Mode, serverCfg.Port); err != nil {
		return err
	}

	addr := listenAddr(serverCfg)
	logger.Scene("init").With("addr", addr).Info("服务启动")

	srv := buildHTTPServer(serverCfg, router)

	serverErrCh := serveHTTPServer(srv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-serverErrCh:

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Scene("init").Error(err, "HTTP Server 关闭失败")
	}

	if err := config.CloseComponents(); err != nil {
		logger.Scene("init").Error(err, "组件关闭失败")
	}

	logger.Scene("init").Info("服务已退出")
	return nil
}

func runPartitionMaintenance() error {
	db, err := database.GetDB()
	if err != nil {
		return fmt.Errorf("分区维护前置检查失败: %w", err)
	}
	created, err := partition.EnsureAhead(context.Background(), db, partition.AheadMonths)
	if err != nil {
		return fmt.Errorf("分区维护失败: %w", err)
	}
	if len(created) > 0 {
		logger.Scene("init").With("count", len(created)).Info("已补齐未来分区（-migrate-only）")
	}
	return nil
}

func runSeeds() error {
	db, err := database.GetDB()
	if err != nil {
		return fmt.Errorf("seed 前置检查失败: %w", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		return fmt.Errorf("业务 seed 失败: %w", err)
	}
	return nil
}

func loadAndPrepareRuntime(configPath string) (config.ServerConfig, error) {

	if err := config.Init(configPath); err != nil {
		return config.ServerConfig{}, fmt.Errorf("配置加载失败: %w", err)
	}

	serverCfg, err := config.GetServer()
	if err != nil {
		return config.ServerConfig{}, err
	}

	gin.SetMode(serverCfg.Mode)

	if err := config.InitComponents(); err != nil {
		return config.ServerConfig{}, fmt.Errorf("组件初始化失败: %w", err)
	}

	return serverCfg, nil
}

func buildHTTPRouter(
	ready func() error,
) *gin.Engine {
	router := gin.New()
	if gin.Mode() == gin.ReleaseMode {
		if err := router.SetTrustedProxies(nil); err != nil {
			logger.Scene("init").Error(err, "设置 TrustedProxies 失败")
		} else {
			logger.Scene("init").Info("release 模式：不信任任何代理，ClientIP 取直连 RemoteAddr")
		}
	} else {
		logger.Scene("init").Info("debug/test 模式：保持 Gin 默认信任所有代理，X-Forwarded-For 可伪造 ClientIP，仅供本地调试")
	}

	middleware.Setup(router)

	routers.SetupRoutes(router, ready)
	return router
}

func listenAddr(cfg config.ServerConfig) string {
	if gin.Mode() == gin.ReleaseMode {
		return fmt.Sprintf(":%d", cfg.Port)
	}
	if cfg.DebugAllowPublic {
		logger.Scene("init").With("port", cfg.Port).Warn(
			"debug/test 模式且 server.debug_allow_public=true：绑定全部网络接口。" +
				"该模式下存在一键登录、CORS 反射任意 Origin、信任所有代理三处放宽，" +
				"仅限隔离的开发网络使用；生产环境必须 server.mode=release")
		return fmt.Sprintf(":%d", cfg.Port)
	}
	local := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	logger.Scene("init").With("addr", local).Info(
		"debug/test 模式：仅监听 127.0.0.1（需局域网 / 手机访问时设置 server.debug_allow_public: true）")
	return local
}

func buildHTTPServer(serverCfg config.ServerConfig, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              listenAddr(serverCfg),
		Handler:           handler,
		ReadHeaderTimeout: serverCfg.ReadHeaderTimeout,
		ReadTimeout:       serverCfg.ReadTimeout,
		WriteTimeout:      serverCfg.WriteTimeout,
		IdleTimeout:       serverCfg.IdleTimeout,
	}
}

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
