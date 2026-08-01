#!/usr/bin/env bash
set -euo pipefail

# 运行仓库全部静态检查、服务端测试与前端生产构建。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"

"$project_dir/scripts/check-public-tree.sh"
"$project_dir/scripts/test-nginx-config.sh"
"$project_dir/scripts/test-server-workflow.sh"

grep -Fxq 'module my-travel/server' "$project_dir/server/go.mod" || {
  echo "Go module 必须使用不绑定代码托管主体的项目内命名空间。" >&2
  exit 1
}
module_path_pattern='(github\.com|gitlab\.com|codeberg\.org)/[^[:space:]]+/my-travel/server'
if command -v rg >/dev/null 2>&1; then
  module_path_matches() {
    rg -n "$module_path_pattern" "$project_dir/server" --glob '*.go' --glob 'go.mod'
  }
else
  module_path_matches() {
    grep -r -n -E --include='*.go' --include='go.mod' \
      "$module_path_pattern" "$project_dir/server"
  }
fi
if module_path_matches; then
  echo "项目内部 Go import 不得绑定代码托管平台主体。" >&2
  exit 1
fi

while IFS= read -r script; do
  bash -n "$script"
done < <(
  {
    find scripts -type f -perm -u+x -print
    printf '%s\n' scripts/lib/project-config.sh
  } | sort -u
)

grep -Fq 'run --rm --no-deps -T snapshot-init </dev/null' \
  "$project_dir/scripts/deploy/project-remote.sh" || {
  echo "远程项目脚本的一次性容器必须隔离 SSH 标准输入。" >&2
  exit 1
}

"$project_dir/scripts/test-travel-api.sh"
"$project_dir/scripts/test-travel-data.sh"
"$project_dir/scripts/test-travel-plan.sh"
node --check "$project_dir/.agents/skills/amap-lbs-skill/scripts/amap.mjs"
node "$project_dir/.agents/skills/amap-lbs-skill/scripts/amap.mjs" --help >/dev/null

cd "$project_dir/server"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  "$project_dir/scripts/test-mongo-integration.sh"
else
  echo "未检测到可用 Docker，跳过真实 MongoDB 集成测试。" >&2
  go test ./...
fi
go vet ./...

cd "$project_dir/web"
if [[ ! -d node_modules ]]; then
  npm ci
fi
npm run typecheck
npm run test:coverage
npm run build
npm run lint
npm run audit

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  cd "$project_dir"
  BOOTSTRAP_TOKEN=static-test docker compose config >/dev/null
fi
