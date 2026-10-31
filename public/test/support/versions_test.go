package support

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("解析仓库根失败: %v", err)
	}
	return root
}

func TestCIWorkflowImagesMatchDeclaredVersions(t *testing.T) {
	path := filepath.Join(repoRoot(t), ".github", "workflows", "go-test.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 CI 配置失败: %v", err)
	}
	text := string(data)
	want := CurrentVersionManifest()

	imageRe := regexp.MustCompile(`(?m)^\s*image:\s*(\S+)\s*$`)
	got := map[string]bool{}
	for _, m := range imageRe.FindAllStringSubmatch(text, -1) {
		got[m[1]] = true
	}

	if !hasImageWithPrefix(got, "postgres:") {
		t.Errorf("CI 里没有 postgres 镜像（改了 workflow 结构？扫到 %v）", keys(got))
	}
	for name := range got {
		switch {
		case strings.HasPrefix(name, "postgres:"):
			if name != want.PostgresImage {
				t.Errorf("CI 的 PostgreSQL 镜像是 %s，代码声明的是 %s —— 两处已经分叉："+
					"CI 测的版本与 testcontainers 拉起的版本必须一致，否则本地能过、CI 不能过（或反之）",
					name, want.PostgresImage)
			}
		case strings.HasPrefix(name, "redis:"):

			if !sameMajor(name, want.RedisImage) {
				t.Errorf("CI 的 Redis 镜像 %s 与代码声明 %s 不是同一大版本", name, want.RedisImage)
			}
		}
	}
}

func hasImageWithPrefix(set map[string]bool, prefix string) bool {
	for name := range set {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func majorOf(image string) string {
	i := strings.LastIndex(image, ":")
	if i < 0 {
		return ""
	}
	tag := image[i+1:]
	if j := strings.IndexByte(tag, '-'); j >= 0 {
		tag = tag[:j]
	}
	return tag
}

func sameMajor(a, b string) bool { return majorOf(a) == majorOf(b) }

func TestProductionVersionsMatchTests(t *testing.T) {
	raw := strings.TrimSpace(os.Getenv("GOWP_PRODUCTION_VERSIONS"))
	prod, err := ParseProductionVersions(raw)
	if err != nil {
		t.Fatalf("生产版本声明解析失败: %v", err)
	}

	if !prod.Declared {

		t.Log("⚠ 生产版本未声明（GOWP_PRODUCTION_VERSIONS 为空）—— " +
			"CI 与生产的 PG/Redis 版本是否一致**未经核对**。" +
			"部署时请按实际版本设置该变量，例如 postgres=18,redis=8")
		return
	}

	if prod.PostgresMajor != 0 && prod.PostgresMajor != TestPostgresMajor {
		t.Errorf("生产 PostgreSQL %d ≠ 测试 %d：CI 的结论不能直接外推到生产"+
			"（PG 跨大版本有规划器与 jsonb 行为差异）。要么把生产升到 %d，要么把测试升到 %d",
			prod.PostgresMajor, TestPostgresMajor, TestPostgresMajor, prod.PostgresMajor)
	}
	if prod.RedisMajor != 0 && prod.RedisMajor != TestRedisMajor {
		t.Errorf("生产 Redis %d ≠ 测试 %d", prod.RedisMajor, TestRedisMajor)
	}
}

func TestVersionManifestIsSelfConsistent(t *testing.T) {
	m := CurrentVersionManifest()
	if majorOf(m.PostgresImage) != itoa(TestPostgresMajor) {
		t.Errorf("PostgreSQL 镜像 %s 与常量 %d 不同源", m.PostgresImage, TestPostgresMajor)
	}
	if majorOf(m.RedisImage) != itoa(TestRedisMajor) {
		t.Errorf("Redis 镜像 %s 与常量 %d 不同源", m.RedisImage, TestRedisMajor)
	}

	if testPGImage != m.PostgresImage {
		t.Errorf("testenv 用的 PG 镜像 %s 与清单 %s 不一致", testPGImage, m.PostgresImage)
	}
	if testRedisImage != m.RedisImage {
		t.Errorf("testenv 用的 Redis 镜像 %s 与清单 %s 不一致", testRedisImage, m.RedisImage)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestParseProductionVersions(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    ProductionVersions
		wantErr bool
	}{
		{"", ProductionVersions{}, false},
		{"postgres=18,redis=8", ProductionVersions{PostgresMajor: 18, RedisMajor: 8, Declared: true}, false},
		{" PostgreSQL = 18 , Redis = 8 ", ProductionVersions{PostgresMajor: 18, RedisMajor: 8, Declared: true}, false},
		{"pg=18", ProductionVersions{PostgresMajor: 18, Declared: true}, false},
		{"postgres=18", ProductionVersions{PostgresMajor: 18, Declared: true}, false},
		{"mysql=8", ProductionVersions{}, true},
		{"postgres=abc", ProductionVersions{}, true},
		{"postgres", ProductionVersions{}, true},
	} {
		got, err := ParseProductionVersions(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q 应报错，却解析成 %+v", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q 解析失败: %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q 解析成 %+v，期望 %+v", tc.raw, got, tc.want)
		}
	}
}
