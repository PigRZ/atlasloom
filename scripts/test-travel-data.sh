#!/usr/bin/env bash
set -euo pipefail

# 用本地 mock 验证攻略工作区不会覆盖未同步内容，并正确传递快照基准。

if [[ "$(basename "$0")" == "mock-api" ]]; then
  if [[ -n "${TRAVEL_DATA_API_LOG:-}" ]]; then
    printf '%s\n' "$*" >> "$TRAVEL_DATA_API_LOG"
  fi
  case "${1:-} ${2:-}" in
    "trips list")
      if [[ -n "${TRAVEL_DATA_TRIPS_RESPONSE:-}" ]]; then
        printf '%s\n' "$TRAVEL_DATA_TRIPS_RESPONSE"
      else
        printf '{"trips":[{"id":"trip-1","role":"owner"},{"id":"trip-2"}]}\n'
      fi
      ;;
    "session ")
      printf '{"user":{"admin":%s}}\n' "${TRAVEL_DATA_MOCK_ADMIN:-true}"
      ;;
    "library snapshot")
      printf '{"revision":3,"contentChecksum":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","datasets":{"members":[],"tags":[]}}\n'
      ;;
    "snapshots list")
      printf '{"snapshots":[{"id":"snapshot-1"}]}\n'
      ;;
    "snapshots download")
      cp "$TRAVEL_DATA_FIXTURE" "$5"
      ;;
    "trips backup")
      cp "$TRAVEL_DATA_BACKUP_FIXTURE" "$4"
      ;;
    "trips get")
      printf '{"trip":{"version":7}}\n'
      ;;
    "snapshot-imports preview")
      printf '{"valid":true,"baseRevision":7,"currentRevision":7,"checksum":"%s","deleteCount":0,"resources":{},"membershipAccess":{"created":[],"deleted":[]},"hasChanges":%s,"message":"preview"}\n' \
        "$(jq -r '.contentChecksum' "$5")" \
        "${TRAVEL_DATA_PREVIEW_HAS_CHANGES:-false}"
      ;;
    *)
      echo "mock API 未覆盖：$*" >&2
      exit 2
      ;;
  esac
  exit 0
fi

if [[ "$(basename "$0")" == "mock-ctl" ]]; then
  case "$1" in
    validate-snapshot)
      jq -e '.revision == 7' "$3" >/dev/null
      ;;
    validate-trip-backup)
      jq -e '
        .kind == "my-travel-trip-backup"
        and (.contentChecksum | test("^[a-f0-9]{64}$"))
      ' "$3" >/dev/null
      ;;
    prepare-snapshot)
      cp "$3" "$5"
      ;;
    diff-snapshots)
      printf '{"days":{"created":0,"updated":0,"deleted":0}}\n'
      ;;
    *)
      echo "mock travelctl 未覆盖：$*" >&2
      exit 2
      ;;
  esac
  exit 0
fi

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/travel-data-test.XXXXXX")"
trap 'rm -rf "$test_dir"' EXIT

fixture="$test_dir/snapshot.json"
cat > "$fixture" <<'JSON'
{
  "revision": 7,
  "contentChecksum": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "exportedAt": "2026-07-26T00:00:00Z",
  "reason": "test",
  "trip": {"id": "trip-1"},
  "datasets": {}
}
JSON
backup_fixture="$test_dir/admin-backup"
cat > "$backup_fixture" <<'JSON'
{
  "kind": "my-travel-trip-backup",
  "exportedAt": "2026-07-26T00:00:00Z",
  "contentChecksum": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
  "snapshot": {
    "revision": 7,
    "contentChecksum": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
    "exportedAt": "2026-07-26T00:00:00Z",
    "reason": "manual-backup",
    "trip": {"id": "trip-2"},
    "datasets": {}
  },
  "dependencies": {"members": [], "tags": []}
}
JSON
chmod 600 "$fixture" "$backup_fixture"

mock_api="$test_dir/mock-api"
mock_ctl="$test_dir/mock-ctl"
api_log="$test_dir/api.log"
cp "$project_dir/scripts/test-travel-data.sh" "$mock_api"
cp "$project_dir/scripts/test-travel-data.sh" "$mock_ctl"
chmod 700 "$mock_api" "$mock_ctl"
: > "$api_log"

export TRAVEL_API_BIN="$mock_api"
export TRAVELCTL_BIN="$mock_ctl"
export TRAVEL_DATA_STATE_DIR="$test_dir/state"
export TRAVEL_DATA_FIXTURE="$fixture"
export TRAVEL_DATA_BACKUP_FIXTURE="$backup_fixture"
export TRAVEL_DATA_API_LOG="$api_log"
"$project_dir/scripts/travel-data" pull trip-1 >/dev/null
"$project_dir/scripts/travel-data" status trip-1 | grep -Fq 'status=in-sync'
# 缺少访问模式的工作区必须停止，不能按隐含旧规则继续使用。
mv "$test_dir/state/trip-1/.access-mode" "$test_dir/trip-1.access-mode"
if "$project_dir/scripts/travel-data" status trip-1 >/dev/null 2>&1; then
  echo "缺少访问模式的工作区不应继续读取。" >&2
  exit 1
fi
mv "$test_dir/trip-1.access-mode" "$test_dir/state/trip-1/.access-mode"
# 模拟一个已经失去 owner 权限、但没有未同步修改的 owner 工作区。
mkdir -p "$test_dir/state/trip-2"
jq '.trip.id = "trip-2"' "$fixture" > "$test_dir/state/trip-2/base.json"
cp "$test_dir/state/trip-2/base.json" "$test_dir/state/trip-2/working.json"
printf 'owner-snapshot\n' > "$test_dir/state/trip-2/.access-mode"
chmod 600 "$test_dir/state/trip-2/base.json" \
  "$test_dir/state/trip-2/working.json" \
  "$test_dir/state/trip-2/.access-mode"
"$project_dir/scripts/travel-data" pull-all >/dev/null
[[ -f "$test_dir/state/trip-1/base.json" && -f "$test_dir/state/trip-1/working.json" ]]
[[ ! -e "$test_dir/state/trip-2/base.json" && ! -e "$test_dir/state/trip-2/working.json" ]]
[[ -f "$test_dir/state/trip-2/admin-backup" && -f "$test_dir/state/trip-2/readonly.json" ]]
[[ "$(<"$test_dir/state/trip-2/.access-mode")" == "admin-backup" ]]
[[ -f "$test_dir/state/library/base.json" ]]
[[ "$(jq -r '.trip.id' "$test_dir/state/trip-2/readonly.json")" == "trip-2" ]]
"$project_dir/scripts/travel-data" status trip-2 | grep -Fq 'status=read-only-backup'
grep -Fq 'trips backup trip-2' "$api_log"
if grep -Fq 'snapshots list trip-2' "$api_log"; then
  echo "管理员非 owner 旅行不应调用 owner 专用快照接口。" >&2
  exit 1
fi
if "$project_dir/scripts/travel-data" sync trip-2 >/dev/null 2>&1; then
  echo "管理员只读备份不应允许 sync。" >&2
  exit 1
fi
if grep -Fq 'snapshot-imports preview trip-2' "$api_log"; then
  echo "管理员只读备份的 sync 必须在本地停止，不能调用写入预览。" >&2
  exit 1
fi
trip2_owner_fixture="$test_dir/trip-2-owner-snapshot.json"
jq '.snapshot' "$backup_fixture" > "$trip2_owner_fixture"
chmod 600 "$trip2_owner_fixture"
TRAVEL_DATA_FIXTURE="$trip2_owner_fixture" \
  "$project_dir/scripts/travel-data" pull trip-2 >/dev/null
[[ "$(<"$test_dir/state/trip-2/.access-mode")" == "owner-snapshot" ]]
[[ -f "$test_dir/state/trip-2/base.json" && -f "$test_dir/state/trip-2/working.json" ]]
[[ ! -e "$test_dir/state/trip-2/admin-backup" && ! -e "$test_dir/state/trip-2/readonly.json" ]]

"$project_dir/scripts/travel-data" diff trip-1 | grep -Fq '"days"'

printf '\n' >> "$test_dir/state/trip-1/working.json"
if "$project_dir/scripts/travel-data" pull trip-1 >/dev/null 2>&1; then
  echo "pull 不应覆盖有改动的 working.json。" >&2
  exit 1
fi

sync_output="$("$project_dir/scripts/travel-data" sync trip-1)"
grep -Fq '已同步，无需写入' <<<"$sync_output"
cmp -s "$test_dir/state/trip-1/base.json" "$test_dir/state/trip-1/working.json"

jq '.trip.title = "本地语义变更"' "$test_dir/state/trip-1/working.json" \
  > "$test_dir/working.changed"
mv "$test_dir/working.changed" "$test_dir/state/trip-1/working.json"
chmod 600 "$test_dir/state/trip-1/working.json"
cp "$test_dir/state/trip-1/working.json" "$test_dir/working.expected"
if TRAVEL_DATA_PREVIEW_HAS_CHANGES=true \
  "$project_dir/scripts/travel-data" sync trip-1 >/dev/null 2>&1; then
  echo "有语义变化的 sync 不应绕过交互确认。" >&2
  exit 1
fi
cmp -s "$test_dir/working.expected" "$test_dir/state/trip-1/working.json"
non_interactive_output="$(
  printf 'CANCEL\n' | TRAVEL_DATA_PREVIEW_HAS_CHANGES=true \
    "$project_dir/scripts/travel-data" sync trip-1 2>&1 || true
)"
if ! grep -Fq '需要交互终端确认' <<<"$non_interactive_output"; then
  echo "非交互同步必须在确认入口安全停止。" >&2
  exit 1
fi

restore_output="$("$project_dir/scripts/travel-data" restore trip-1 "$fixture")"
grep -Fq '已同步，无需写入' <<<"$restore_output"
cmp -s "$test_dir/working.expected" "$test_dir/state/trip-1/working.json"
if grep -Fq 'snapshot-imports apply' "$api_log"; then
  echo "hasChanges=false 时不应调用 snapshot-imports apply。" >&2
  exit 1
fi

if "$project_dir/scripts/travel-data" status '../unsafe' >/dev/null 2>&1; then
  echo "不安全 trip-id 未被拒绝。" >&2
  exit 1
fi

restricted_log="$test_dir/restricted-api.log"
: > "$restricted_log"
if TRAVEL_DATA_STATE_DIR="$test_dir/restricted-state" \
  TRAVEL_DATA_API_LOG="$restricted_log" \
  TRAVEL_DATA_TRIPS_RESPONSE='{"trips":[{"id":"trip-editor","role":"editor"}]}' \
  TRAVEL_DATA_MOCK_ADMIN=false \
  "$project_dir/scripts/travel-data" pull-all >/dev/null 2>&1; then
  echo "非管理员 editor 不应通过 pull-all 取得完整载荷。" >&2
  exit 1
fi
if grep -Fq 'library snapshot' "$restricted_log" ||
  [[ -e "$test_dir/restricted-state" ]]; then
  echo "mixed-role 权限预检失败时不应替换任何本地文件。" >&2
  exit 1
fi

incomplete_log="$test_dir/incomplete-api.log"
incomplete_state="$test_dir/incomplete-state"
mkdir -p "$incomplete_state/trip-owner"
cp "$fixture" "$incomplete_state/trip-owner/base.json"
: > "$incomplete_log"
if TRAVEL_DATA_STATE_DIR="$incomplete_state" \
  TRAVEL_DATA_API_LOG="$incomplete_log" \
  TRAVEL_DATA_TRIPS_RESPONSE='{"trips":[{"id":"trip-owner","role":"owner"}]}' \
  TRAVEL_DATA_MOCK_ADMIN=false \
  "$project_dir/scripts/travel-data" pull-all >/dev/null 2>&1; then
  echo "不完整 owner 工作区不应被 pull-all 覆盖。" >&2
  exit 1
fi
if grep -Fq 'library snapshot' "$incomplete_log" ||
  [[ ! -f "$incomplete_state/trip-owner/base.json" ||
    -e "$incomplete_state/trip-owner/working.json" ]]; then
  echo "不完整工作区必须在任何文件替换前停止。" >&2
  exit 1
fi

echo "travel-data 工作流检查通过"
