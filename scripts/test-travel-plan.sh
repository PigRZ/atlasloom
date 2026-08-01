#!/usr/bin/env bash
set -euo pipefail

# 用隔离目录和 mock travelctl 验证 AI 旅程包装脚本的固定路径与防覆盖行为。

if [[ "${TRAVEL_PLAN_MOCK:-false}" == "true" ]]; then
  [[ "$1" == "build-trip-backup" && "$2" == "--input" && "$4" == "--output" ]]
  [[ -f "$3" ]]
  printf '{"kind":"my-travel-trip-backup"}\n' > "$5"
  chmod 600 "$5"
  exit 0
fi

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/travel-plan-test.XXXXXX")"
trap 'rm -rf "$test_dir"' EXIT

file_mode() {
  local target="$1"
  if stat -c '%a' "$target" >/dev/null 2>&1; then
    stat -c '%a' "$target"
  else
    stat -f '%Lp' "$target"
  fi
}

trip_dir="$test_dir/sample-trip"
mkdir -p "$trip_dir"
printf '{"trip":{"id":"sample-trip"}}\n' > "$trip_dir/plan.json"
chmod 644 "$trip_dir/plan.json"

TRAVEL_PLAN_ROOT="$test_dir" \
TRAVELCTL_BIN="$project_dir/scripts/test-travel-plan.sh" \
TRAVEL_PLAN_MOCK=true \
  "$project_dir/scripts/travel-plan" build sample-trip >/dev/null

[[ -s "$trip_dir/trip-backup.json" ]]
[[ "$(file_mode "$trip_dir")" == "700" ]]
[[ "$(file_mode "$trip_dir/plan.json")" == "600" ]]

if TRAVEL_PLAN_ROOT="$test_dir" \
  TRAVELCTL_BIN="$project_dir/scripts/test-travel-plan.sh" \
  TRAVEL_PLAN_MOCK=true \
  "$project_dir/scripts/travel-plan" build sample-trip >/dev/null 2>&1; then
  echo "travel-plan 覆盖了已存在的备份。" >&2
  exit 1
fi

echo "travel-plan 固定路径与防覆盖检查通过"
