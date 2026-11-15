// Command baseline-manifest 打印基线要覆盖的两段 version，并校验冻结前缀。
//
// 不带参数时输出即 schema/baseline/manifest.txt 的正文：先 [migrations] 段（全部注册
// 迁移），再 [seeds] 段（全部注册 seed），段内顺序即执行顺序。
//
// -check-prefix FILE 校验 FILE 里的两段各自是当前注册表的**前缀**：段内每一项都在
// 同一位置上，允许注册表更长。基线之外的版本照常执行，所以「新增迁移」不是过期；
// 只有顺序错位、条目消失或段内被改写才算不一致。退出码 0 表示通过，1 表示不一致，
// 2 表示读文件失败。
package main

import (
	"flag"
	"fmt"
	"os"

	"go_wp/public/migrations"
	baseline "go_wp/schema/baseline"
)

func main() {
	checkPrefix := flag.String("check-prefix", "", "校验该 manifest 文件是否为当前注册表前缀")
	flag.Parse()

	if *checkPrefix == "" {
		fmt.Println("[migrations]")
		for _, m := range migrations.All() {
			fmt.Println(m.Version)
		}
		fmt.Println("[seeds]")
		for _, s := range migrations.AllSeeds() {
			fmt.Println(s.Version)
		}
		return
	}

	raw, err := os.ReadFile(*checkPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ 读不到 %s：%v\n", *checkPrefix, err)
		os.Exit(2)
	}
	frozen := baseline.ParseManifestText(string(raw))
	migrationsDone := versionsOfMigrations()
	seedsDone := versionsOfSeeds()
	if err := checkPrefixSection("migrations", frozen.Migrations, migrationsDone); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %s：%v\n", *checkPrefix, err)
		os.Exit(1)
	}
	if err := checkPrefixSection("seeds", frozen.Seeds, seedsDone); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %s：%v\n", *checkPrefix, err)
		os.Exit(1)
	}
	fmt.Printf("prefix_ok=1 migrations=%d/%d seeds=%d/%d extras=%d\n",
		len(frozen.Migrations), len(migrationsDone), len(frozen.Seeds), len(seedsDone),
		len(migrationsDone)-len(frozen.Migrations)+len(seedsDone)-len(frozen.Seeds))
}

func versionsOfMigrations() []string {
	all := migrations.All()
	out := make([]string, 0, len(all))
	for _, m := range all {
		out = append(out, m.Version)
	}
	return out
}

func versionsOfSeeds() []string {
	all := migrations.AllSeeds()
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.Version)
	}
	return out
}

func checkPrefixSection(name string, frozen, registry []string) error {
	if len(frozen) > len(registry) {
		return fmt.Errorf("%s 段有 %d 条，注册表只有 %d 条", name, len(frozen), len(registry))
	}
	for i, want := range frozen {
		if registry[i] != want {
			return fmt.Errorf("%s 段第 %d 条应是 %q，注册表里是 %q —— 基线之后的版本不能插到已冻结的版本之前，也不能改写已冻结的条目", name, i+1, want, registry[i])
		}
	}
	return nil
}
