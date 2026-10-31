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

var (
	testPGImage    = TestPostgresImage()
	testRedisImage = TestRedisImage()
)

var ErrEnvUnavailable = errors.New("测试环境不可用")

type TestEnv struct {
	PG          PGEndpoint
	PGSource    string
	RedisAddr   string
	RedisSource string
}

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

func AcquirePG(t *testing.T) (PGEndpoint, string, error) {
	t.Helper()
	ep, source, err := acquirePG(t)
	FailIfRequiredPG(t, err)
	return ep, source, err
}

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

func pingPG(ep PGEndpoint) error {
	dsn := pgAppendParams(pgDSN(ep.Host, ep.Port, ep.User, ep.Password, ep.Database), [2]string{"connect_timeout", "5"})
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
