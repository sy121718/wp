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
	"sync"
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
		// migrate-only 也要走统一收尾：失败不能绕过显式关闭。
		seedErr := error(nil)
		if config.RunMigrationsEnabled() {
			if seedErr = runSeeds(); seedErr == nil {
				seedErr = runPartitionMaintenance()
			}
		}
		closeErr := config.CloseComponents()
		if seedErr != nil {
			return errors.Join(seedErr, closeErr)
		}
		if closeErr != nil {
			logger.Scene("init").Error(closeErr, "组件关闭失败")
		}
		logger.Scene("init").Info("数据库迁移与 seed 完成（-migrate-only），未启动 HTTP 服务")
		return nil
	}

	// 后台循环挂在 bgCtx 上：进程收尾时先 cancel 再 Shutdown，
	// 循环不会在库已关闭之后还去领任务。
	bgCtx, stopBackground := context.WithCancel(context.Background())

	// 收尾顺序只有这一处：cancel 后台循环 → 等待本次装配启动的任务退出（在途一轮结束）
	// → 关闭 HTTP 在途请求 → 关闭组件（含数据库）。
	// 正常退出、监听失败、端口准备失败都走它；重复调用幂等，不掩盖原始错误。
	inflight := newInflightTracker()
	router, assembly := buildHTTPRouter(bgCtx, inflight, config.ValidateReady)
	shutdown := &shutdownChain{
		stopBackground: stopBackground, waitTasks: assembly.Stop,
		inflight: inflight.Count, waitInflight: inflight.Wait,
	}
	defer shutdown.run()

	// 装配已经启动后台任务：端口准备失败也必须走「cancel → 等待任务 → 关组件」，
	// 不能只靠 defer 取消就返回（那会让任务在库关闭后仍可能领任务）。
	if err := utils.EnsurePortReadyWithStrategy(serverCfg.PortStrategy, serverCfg.Mode, serverCfg.Port); err != nil {
		shutdown.run()
		return err
	}

	addr := listenAddr(serverCfg)
	logger.Scene("init").With("addr", addr).Info("服务启动")

	srv := buildHTTPServer(serverCfg, router)
	shutdown.shutdownHTTP = func(ctx context.Context) error { return srv.Shutdown(ctx) }

	serverErrCh := serveHTTPServer(srv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-serverErrCh:

		// 监听失败也要走同一条停止链：先 cancel 后台循环，再等它们退出，最后关组件。
		shutdown.run()
		if err != nil {
			return fmt.Errorf("HTTP 服务启动失败: %w", err)
		}
		return nil
	case <-quit:
		logger.Scene("init").Info("收到退出信号，开始关闭...")
	}

	shutdown.run()
	logger.Scene("init").Info("服务已退出")
	return nil
}

// shutdownChain 是一次进程启动拥有的收尾顺序，只执行一次。
type shutdownChain struct {
	stopBackground func()
	waitTasks      func()
	shutdownHTTP   func(context.Context) error

	// inflight 报告仍在执行的处理器数量。shutdownHTTP 超时返回时处理器可能还在跑，
	// 此时直接关数据库会让它们读到已关闭的连接；必须等它们退出或明确放弃。
	inflight func() int
	// waitInflight 阻塞到在途请求退出，或 ctx 结束。
	waitInflight func(context.Context)

	// closeComponents 关闭依赖（含数据库）。nil 时用真实 config.CloseComponents；
	// 测试注入替代实现以观察关闭时机。
	closeComponents func()

	// httpTimeout 覆盖 http.Server.Shutdown 的等待时长（0 用默认值）。
	// 生产用 5s；测试可调短，让「Shutdown 真的超时」可被复现，而不是靠桩返回错误。
	httpTimeout time.Duration

	// drainTimeout 覆盖在途排空的兜底等待时长（0 用默认值，测试可调短）。
	drainTimeout time.Duration

	once sync.Once
}

// 关闭两段超时：先让 http.Server 停止接收并排空，再给仍在执行的处理器一段兜底等待。
const (
	shutdownHTTPTimeout  = 5 * time.Second
	shutdownDrainTimeout = 10 * time.Second
)

// run 是幂等的统一收尾：cancel → 等任务 → 关 HTTP（含在途排空）→ 关组件。
func (s *shutdownChain) run() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.stopBackground != nil {
			s.stopBackground()
		}
		if s.waitTasks != nil {
			// 等待在途一轮结束，之后才允许关闭数据库。
			s.waitTasks()
		}
		if s.shutdownHTTP != nil {
			timeout := s.httpTimeout
			if timeout <= 0 {
				timeout = shutdownHTTPTimeout
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if err := s.shutdownHTTP(ctx); err != nil {
				logger.Scene("init").Error(err, "HTTP Server 关闭失败")
			}
			// Shutdown 超时/失败时处理器可能仍在执行：直接关数据库会让它们
			// 读到已关闭的连接。这里显式再等一段，仍有残留就记录放弃数量 ——
			// 「等不到」是明确决定，不是静默跳过。
			if s.inflight != nil && s.inflight() > 0 {
				drain := s.drainTimeout
				if drain <= 0 {
					drain = shutdownDrainTimeout
				}
				waitCtx, waitCancel := context.WithTimeout(context.Background(), drain)
				if s.waitInflight != nil {
					s.waitInflight(waitCtx)
				}
				waitCancel()
				if left := s.inflight(); left > 0 {
					logger.Scene("init").
						With("inflight", left).
						Error(nil, "HTTP 关闭后仍有在途请求未退出，超时放弃等待并关闭组件（这些请求会拿到连接已关闭的错误）")
				}
			}
		}
		closeComponents := s.closeComponents
		if closeComponents == nil {
			closeComponents = func() {
				if err := config.CloseComponents(); err != nil {
					logger.Scene("init").Error(err, "组件关闭失败")
				}
			}
		}
		closeComponents()
	})
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

// buildHTTPRouter 装配路由并返回本次装配句柄：收尾时先 cancel bgCtx，
// 再调 assembly.Stop() 等待后台任务退出，最后才关闭组件（含数据库）。
func buildHTTPRouter(
	bgCtx context.Context,
	inflight *inflightTracker,
	ready func() error,
) (*gin.Engine, *routers.Assembly) {
	router := gin.New()
	// 在途追踪必须是**最外层**中间件，且要在任何路由注册之前挂：
	// Gin 在注册时固定处理器链，注册后再 Use 不会进入已有链路。
	if inflight != nil {
		router.Use(inflight.Middleware())
	}
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

	assembly := routers.StartAssembly(bgCtx, router, ready)
	return router, assembly
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
