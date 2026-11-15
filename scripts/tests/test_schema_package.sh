#!/usr/bin/env bash
# public/schema 的静态判据：包内测试（段序 / 切分 / order 覆盖）与摘要稳定性。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

go test ./public/schema/ -count=1

first=$(go run ./cmd/schema-digest | sed -n '1p')
second=$(go run ./cmd/schema-digest | sed -n '1p')
version=$(go run ./cmd/schema-digest | sed -n '2p')

[ "${#first}" -eq 64 ] || { echo "✗ 摘要长度不是 64：${#first}" >&2; exit 1; }
[ "$first" = "$second" ] || { echo "✗ 摘要不稳定：$first vs $second" >&2; exit 1; }
[ "$version" = "schema-${first:0:8}" ] || { echo "✗ 版本号与摘要不同源：$version" >&2; exit 1; }
echo "✓ public/schema 摘要稳定：$version"
