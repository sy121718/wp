#!/usr/bin/env bash
# Go vet 全量（含**测试文件**的编译检查）。
#
# 为什么需要它：`go build ./...` 不编译测试文件 —— 改了契约接口（例如给
# ordercontract.OrderOverviewReader 加一个方法）之后，业务代码全绿，而某个包里手工写的
# stub 不满足那个接口，**整个包的测试编译不过**。这种失败只有到 `go test ./...` 才会暴露，
# 而本项目的门禁脚本此前没有一个跑它。
#
# 实测（P6-b2）：给 OrderOverviewReader 加 SoldQuantityByRange 后推了一版
# 测试编译不过的代码到 main —— `go build ./...` 通过、14 个门禁全绿，直到下一批
# 跑这个包的测试才发现。判据是 `go vet`：它既做静态检查，也编译测试文件。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

out="$(go vet ./... 2>&1)"
if [[ -n "$out" ]]; then
  echo "$out" >&2
  echo "✗ go vet 未通过（go build 不编译测试文件，契约改动后 stub 失配只在这里暴露）" >&2
  exit 1
fi
echo "✓ go vet 通过（含测试文件编译）"
