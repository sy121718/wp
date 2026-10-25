package support

// versions.go — 测试环境的 PostgreSQL / Redis 版本**单一真源**。
//
// 要解决的问题：版本号原先散在三处（testenv.go 的容器镜像常量、CI workflow 的
// services.postgres.image、CI 注释里的「与生产对齐」），三处各写各的，谁也说不出
// 它们什么时候会分叉。而分叉的后果是**测试绿不代表生产绿**：PG 17/18 在查询规划、
// jsonb 函数、MERGE 语义上都有行为差异，CI 测 PG 16、生产跑 PG 18 时，
// 「本地/CI 都过」这件事对生产几乎没有信息量。
//
// 做法：
//  1. 版本只在这里声明一次（Go 侧常量 + 机器可读的清单）；
//  2. CI 的 workflow 文件由测试对账（versions_test.go），不再靠人记；
//  3. 生产对齐这件事无法从代码判断（生产版本不在仓里），所以提供一个显式声明处：
//     PRODUCTION_VERSIONS 环境变量 / 部署清单。未声明时测试**明确报告「未声明」**，
//     而不是假装对齐了 —— 假装对齐比不知道更糟。
//
// 修改流程：改这里的常量 → 跑 public/test/support 的测试 → 它会告诉你 CI 哪一行要跟着改。

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// TestPostgresMajor/Minor 测试与 CI 使用的 PostgreSQL 大版本。
	//
	// 大版本必须与**生产**一致（见 ProductionVersionDeclared）。小版本不必严格对齐：
	// 同一大版本内的 patch 差异不该改变可观察行为。
	TestPostgresMajor = 16

	// TestRedisMajor 测试与 CI 使用的 Redis 大版本。同上。
	TestRedisMajor = 7
)

// TestPostgresImage testcontainers 拉起的 PG 镜像（与 TestPostgresMajor 必须同源）。
func TestPostgresImage() string { return fmt.Sprintf("postgres:%d", TestPostgresMajor) }

// TestRedisImage testcontainers 拉起的 Redis 镜像。
func TestRedisImage() string { return fmt.Sprintf("redis:%d-alpine", TestRedisMajor) }

// VersionManifest 环境版本清单（供测试对账 CI workflow 用）。
type VersionManifest struct {
	PostgresImage string
	RedisImage    string
}

// CurrentVersionManifest 当前代码声明的版本清单。
func CurrentVersionManifest() VersionManifest {
	return VersionManifest{
		PostgresImage: TestPostgresImage(),
		RedisImage:    TestRedisImage(),
	}
}

// ProductionVersions 生产版本声明（由部署方通过环境变量提供）。
type ProductionVersions struct {
	// PostgresMajor 生产 PostgreSQL 大版本；0 表示未声明。
	PostgresMajor int
	// RedisMajor 生产 Redis 大版本；0 表示未声明。
	RedisMajor int
	// Declared 是否至少声明了一项。
	Declared bool
}

// ParseProductionVersions 解析部署方给出的生产版本声明。
//
// 形如 "postgres=18,redis=8"（大小写不敏感、允许空格）。生产环境是哪个版本，
// 仓里查不到 —— 只有部署的人知道，所以这件事必须由部署方显式声明，不能猜。
//
// 为什么不做「默认对齐测试版本」：那会让「未声明」与「已对齐」在测试里长得一样，
// 而这两件事的信息量完全不同。
func ParseProductionVersions(raw string) (ProductionVersions, error) {
	out := ProductionVersions{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, nil
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return out, fmt.Errorf("无法解析版本声明 %q：应为 key=value 形式（如 postgres=18）", part)
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		n, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil || n <= 0 {
			return out, fmt.Errorf("版本声明 %q 的数值不合法", part)
		}
		switch key {
		case "postgres", "postgresql", "pg":
			out.PostgresMajor = n
		case "redis":
			out.RedisMajor = n
		default:
			return out, fmt.Errorf("未知的组件名 %q（只认 postgres / redis）", key)
		}
		out.Declared = true
	}
	return out, nil
}
