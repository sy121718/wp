// testenv.go — feature 测试的 PG/Redis 环境接入 helper（三级回退）：
//
//  1. 优先复用本地服务：PG 沿用 pgtest.go 的连接配置（config.yaml 对齐值 +
//     PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE 环境变量覆盖），Redis 沿用
//     admin_session.go 的 TEST_REDIS_ADDR / DefaultTestRedisAddr；
//  2. 本地不可用且 Docker 可用时，用 testcontainers-go 自动拉起 postgres:16 /
//     redis:7 容器并等待就绪；容器在同一测试二进制内共享（sync.Once 缓存），
//     由 ShutdownSharedTestEnv 统一终止；
//  3. 两级都失败才返回包装 ErrEnvUnavailable 的错误，调用方 t.Skip
//     （Skip 消息写明本地与容器两级的具体失败原因）。
//
// 容器清理顺序契约（配合 goleak.go）：接入 TestMain 的包由
// RunTestMainWithLeakCheck 在 m.Run() 返回后、泄漏检测前调用
// ShutdownSharedTestEnv，保证容器后台 goroutine 不参与 leak check；
// 未接 TestMain 的包由 testcontainers ryuk 在进程退出时兜底回收。
package support

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 容器镜像：与生产主库/会话存储大版本对齐（PostgreSQL 16 / Redis 7）。
const (
	testPGImage    = "postgres:16"
	testRedisImage = "redis:7-alpine"
)

// ErrEnvUnavailable 表示本地与容器两级环境均不可用，测试应 t.Skip 而非 fail。
var ErrEnvUnavailable = errors.New("测试环境不可用")

// TestEnv 一份已就绪的 PG + Redis 测试环境端点（本地或容器）。
type TestEnv struct {
	PG          PGEndpoint // PG 连接参数
	PGSource    string     // PG 来源：local | container
	RedisAddr   string     // Redis 地址 host:port
	RedisSource string     // Redis 来源：local | container
}

// AcquireTestEnv 获取 PG + Redis 双资源测试环境（feature 链路测试常用组合）。
// 每级资源独立三级回退；任一资源两级都失败即返回包装 ErrEnvUnavailable 的
// 错误（消息含本地与容器具体原因），调用方据此 t.Skip。
func AcquireTestEnv(t *testing.T) (*TestEnv, error) {
	t.Helper()

	env := &TestEnv{}
	pgEP, pgSource, pgErr := acquirePG(t)
	if pgErr != nil {
		FailIfRequiredPG(t, pgErr)
		return nil, pgErr
	}
	env.PG, env.PGSource = pgEP, pgSource

	redisAddr, redisSource, redisErr := acquireRedis(t)
	if redisErr != nil {
		FailIfRequiredRedis(t, redisErr)
		return nil, redisErr
	}
	env.RedisAddr, env.RedisSource = redisAddr, redisSource
	return env, nil
}

// AcquirePG 仅获取 PG 测试环境（只依赖 PG 的测试用）。
// 返回端点、来源（local/container）；两级失败时返回包装 ErrEnvUnavailable 的错误。
func AcquirePG(t *testing.T) (PGEndpoint, string, error) {
	t.Helper()
	ep, source, err := acquirePG(t)
	FailIfRequiredPG(t, err)
	return ep, source, err
}

// AcquireRedis 仅获取 Redis 测试环境（只依赖 Redis 的测试用）。
// 返回地址、来源（local/container）；两级失败时返回包装 ErrEnvUnavailable 的错误。
func AcquireRedis(t *testing.T) (string, string, error) {
	t.Helper()
	addr, source, err := acquireRedis(t)
	FailIfRequiredRedis(t, err)
	return addr, source, err
}

func acquirePG(t *testing.T) (PGEndpoint, string, error) {
	t.Helper()

	localEP := localPGEndpoint()
	localErr := pingPG(localEP)
	if localErr == nil {
		return localEP, "local", nil
	}
	ep, containerErr := sharedPGContainerEndpoint()
	if containerErr != nil {
		return PGEndpoint{}, "", fmt.Errorf("%w: PG 本地不可用（%v），且容器回退失败（%v）",
			ErrEnvUnavailable, localErr, containerErr)
	}
	return ep, "container", nil
}

func acquireRedis(t *testing.T) (string, string, error) {
	t.Helper()

	localAddr := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if localAddr == "" {
		localAddr = DefaultTestRedisAddr
	}
	localErr := pingRedis(localAddr)
	if localErr == nil {
		return localAddr, "local", nil
	}
	addr, containerErr := sharedRedisContainerAddr()
	if containerErr != nil {
		return "", "", fmt.Errorf("%w: Redis 本地不可用（%v），且容器回退失败（%v）",
			ErrEnvUnavailable, localErr, containerErr)
	}
	return addr, "container", nil
}

// pingPG 探测 PG 端点连通性（connect_timeout=5s，避免不可达地址长时间挂起）。
func pingPG(ep PGEndpoint) error {
	dsn := pgDSN(ep.Host, ep.Port, ep.User, ep.Password, ep.Database) + " connect_timeout=5"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

// pingRedis 探测 Redis 端点连通性（3s 超时）。
func pingRedis(addr string) error {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer client.Close()
	return client.Ping(ctx).Err()
}

var (
	envMu            sync.Mutex
	dockerProbeOnce  sync.Once
	dockerProbeErr   error
	sharedPGOnce     sync.Once
	sharedPGEP       PGEndpoint
	sharedPGErr      error
	sharedRedisOnce  sync.Once
	sharedRedisAddr  string
	sharedRedisErr   error
	sharedContainers []testcontainers.Container
	sharedShutdown   bool
)

func sharedPGContainerEndpoint() (PGEndpoint, error) {
	sharedPGOnce.Do(func() { sharedPGEP, sharedPGErr = startPGContainer() })
	return sharedPGEP, sharedPGErr
}

func sharedRedisContainerAddr() (string, error) {
	sharedRedisOnce.Do(func() { sharedRedisAddr, sharedRedisErr = startRedisContainer() })
	return sharedRedisAddr, sharedRedisErr
}

// probeDocker 探测 Docker 环境：NewDockerProvider 成功即视为可用（结果缓存）。
func probeDocker() error {
	dockerProbeOnce.Do(func() {
		p, err := testcontainers.NewDockerProvider()
		if err != nil {
			dockerProbeErr = err
			return
		}
		dockerProbeErr = p.Close()
	})
	return dockerProbeErr
}

func startPGContainer() (PGEndpoint, error) {
	if err := probeDocker(); err != nil {
		return PGEndpoint{}, fmt.Errorf("Docker 不可用: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	c, err := tcpostgres.Run(ctx, testPGImage,
		tcpostgres.WithDatabase(DefaultPGDatabase),
		tcpostgres.WithUsername(DefaultPGUser),
		tcpostgres.WithPassword(DefaultPGPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return PGEndpoint{}, fmt.Errorf("拉起 %s 容器失败: %w", testPGImage, err)
	}
	registerSharedContainer(c)

	ep := PGEndpoint{
		User:     DefaultPGUser,
		Password: DefaultPGPassword,
		Database: DefaultPGDatabase,
	}
	if ep.Host, err = c.Host(ctx); err != nil {
		return PGEndpoint{}, fmt.Errorf("读取容器 Host 失败: %w", err)
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return PGEndpoint{}, fmt.Errorf("读取容器映射端口失败: %w", err)
	}
	ep.Port = port.Port()

	// BasicWaitStrategies 已含 wait.ForSQL 就绪等待；此处再 ping 一次，
	// 统一「返回的端点即已就绪」语义（与本地路径一致）。
	if err := pingPG(ep); err != nil {
		return PGEndpoint{}, fmt.Errorf("容器端点连通性验证失败: %w", err)
	}
	return ep, nil
}

func startRedisContainer() (string, error) {
	if err := probeDocker(); err != nil {
		return "", fmt.Errorf("Docker 不可用: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	c, err := tcredis.Run(ctx, testRedisImage)
	if err != nil {
		return "", fmt.Errorf("拉起 %s 容器失败: %w", testRedisImage, err)
	}
	registerSharedContainer(c)

	host, err := c.Host(ctx)
	if err != nil {
		return "", fmt.Errorf("读取容器 Host 失败: %w", err)
	}
	port, err := c.MappedPort(ctx, "6379/tcp")
	if err != nil {
		return "", fmt.Errorf("读取容器映射端口失败: %w", err)
	}
	addr := net.JoinHostPort(host, port.Port())
	if err := pingRedis(addr); err != nil {
		return "", fmt.Errorf("容器端点连通性验证失败: %w", err)
	}
	return addr, nil
}

func registerSharedContainer(c testcontainers.Container) {
	envMu.Lock()
	defer envMu.Unlock()
	sharedContainers = append(sharedContainers, c)
}

// ShutdownSharedTestEnv 终止所有共享测试容器（幂等）。
// 必须在 goleak 泄漏检测之前调用：容器后台 goroutine（ryuk reaper、
// docker client 等）会被 leak check 误判为泄漏。TestMain 模式由
// RunTestMainWithLeakCheck 自动在 m.Run() 之后调用；未接 TestMain 的包
// 由 ryuk 在进程退出时兜底回收。
func ShutdownSharedTestEnv() {
	envMu.Lock()
	defer envMu.Unlock()
	if sharedShutdown {
		return
	}
	sharedShutdown = true
	for _, c := range sharedContainers {
		if err := testcontainers.TerminateContainer(c); err != nil {
			fmt.Fprintf(os.Stderr, "testenv: 终止共享容器失败: %v\n", err)
		}
	}
	sharedContainers = nil
}
