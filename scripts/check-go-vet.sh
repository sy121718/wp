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

# 判据是**退出码**，不是「有没有输出」。
#
# 为什么改：原先用 `out="$(go vet ./... 2>&1)"` 判「输出非空即失败」，而 go 工具链在
# **冷缓存**下会往 stderr 打 `go: downloading ...` —— 那一行被当成诊断，于是
# 「下载依赖」被判成「vet 未通过」。本地有缓存时不复现，CI 上必现（首次 checkout 无缓存）。
# 症状是日志里只有汇总提示、没有任何 go vet 的真实诊断，正是这次审查看到的样子。
#
# 现在：分开收 stdout/stderr，退出码非 0 才失败；下载日志原样透传（有用，能看出卡在哪），
# 但不再参与判定。
vet_err="$(mktemp)"
trap 'rm -f "$vet_err"' EXIT

if ! go vet ./... 2>"$vet_err"; then
  # 真实诊断在 stderr 里（go vet 把诊断写 stderr），也可能是下载失败。
  cat "$vet_err" >&2
  echo "✗ go vet 未通过（go build 不编译测试文件，契约改动后 stub 失配只在这里暴露）" >&2
  exit 1
fi

# 通过时 stderr 里可能只有 `go: downloading ...` 这类进度信息，不打印噪音。
echo "✓ go vet 通过（含测试文件编译）"
