package support

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	TestPostgresMajor = 16

	TestRedisMajor = 7
)

func TestPostgresImage() string { return fmt.Sprintf("postgres:%d", TestPostgresMajor) }

func TestRedisImage() string { return fmt.Sprintf("redis:%d-alpine", TestRedisMajor) }

type VersionManifest struct {
	PostgresImage string
	RedisImage    string
}

func CurrentVersionManifest() VersionManifest {
	return VersionManifest{
		PostgresImage: TestPostgresImage(),
		RedisImage:    TestRedisImage(),
	}
}

type ProductionVersions struct {
	PostgresMajor int

	RedisMajor int

	Declared bool
}

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
