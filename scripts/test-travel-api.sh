#!/usr/bin/env bash
set -euo pipefail

# 用无网络 mock 覆盖 travel-api 的每个命名子命令，防止服务端新增路由后脚本映射失真。

if [[ "${TRAVEL_API_MOCK_CURL:-false}" == "true" ]]; then
  method=GET
  url=
  output=
  data_binary=
  body=
  insecure=false
  headers=()
  while (($# > 0)); do
    case "$1" in
      --request)
        method="$2"
        shift 2
        ;;
      --url)
        url="$2"
        shift 2
        ;;
      --output)
        output="$2"
        shift 2
        ;;
      --header)
        headers+=("${2%%:*}")
        shift 2
        ;;
      --insecure)
        insecure=true
        shift
        ;;
      --data-binary)
        data_binary="$2"
        shift 2
        ;;
      --cookie|--cookie-jar)
        shift 2
        ;;
      *)
        shift
        ;;
    esac
  done
  if [[ "$data_binary" == "@-" ]]; then
    body="$(command cat)"
  elif [[ -n "$data_binary" ]]; then
    echo "mock curl 只接受 --data-binary @-" >&2
    exit 2
  fi
  {
    printf '%s %s insecure=%s headers=' "$method" "$url" "$insecure"
    printf '%s,' "${headers[@]}"
    printf ' body=%s\n' "$body"
  } >> "$TRAVEL_API_MOCK_LOG"
  if [[ -n "$output" ]]; then
    printf '{"revision":1}\n' > "$output"
  elif [[ "$url" == */auth/login ]]; then
    printf '{"user":{"mobile":"13800138000"},"csrfToken":"csrf-test","expiresAt":"2099-01-01T00:00:00Z"}\n'
  elif [[ "${TRAVEL_API_MOCK_PENDING:-false}" == "true" &&
    "$method" != "GET" && "$method" != "HEAD" ]]; then
    printf '{"snapshot":{"mirrorState":"pending"}}\n'
  elif [[ "${TRAVEL_API_MOCK_PENDING_BEFORE:-false}" == "true" &&
    "$method" != "GET" && "$method" != "HEAD" ]]; then
    printf '{"before":{"mirrorState":"pending"},"after":{"mirrorState":"mirrored"}}\n'
  else
    printf '{}\n'
  fi
  exit 0
fi

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/travel-api-test.XXXXXX")"
trap 'rm -rf "$test_dir"' EXIT

config_file="$test_dir/project.env"
mock_log="$test_dir/curl.log"
state_dir="$test_dir/state"
snapshot_file="$test_dir/snapshot.json"
backup_file="$test_dir/trip-backup.json"
trip_import_request="$test_dir/trip-import-request.json"
local_runtime_env="$test_dir/runtime.env"

{
  printf 'API_BASE_URL=http://127.0.0.1:8502\n'
  printf 'API_INSECURE_TLS=true\n'
  printf 'API_DISPLAY_NAME=测试成员\n'
  printf 'API_MOBILE=13800138000\n'
  printf 'ADMIN_PROVISION_TOKEN=test-provision-token\n'
} > "$config_file"
chmod 600 "$config_file"
printf 'BOOTSTRAP_TOKEN=test-bootstrap-token\n' > "$local_runtime_env"
chmod 600 "$local_runtime_env"
: > "$mock_log"

export TRAVEL_CONFIG_FILE="$config_file"
export API_STATE_DIR="$state_dir"
export TRAVEL_API_CURL_BIN="$project_dir/scripts/test-travel-api.sh"
export TRAVEL_API_MOCK_CURL=true
export TRAVEL_API_MOCK_LOG="$mock_log"
export TRAVEL_LOCAL_RUNTIME_ENV="$local_runtime_env"
# 单元测试必须只读取上面的临时配置，不能继承 deploy.sh 已加载的真实目标。
unset API_BASE_URL API_INSECURE_TLS API_DISPLAY_NAME API_MOBILE BOOTSTRAP_TOKEN ADMIN_PROVISION_TOKEN

printf 'secret\nsecret\n' | "$project_dir/scripts/travel-api" auth bootstrap >/dev/null
printf 'secret\nsecret\n' | "$project_dir/scripts/travel-api" auth create-user >/dev/null
printf 'secret\n' | "$project_dir/scripts/travel-api" auth login >/dev/null
"$project_dir/scripts/travel-api" health >/dev/null
"$project_dir/scripts/travel-api" session >/dev/null
"$project_dir/scripts/travel-api" profile get >/dev/null
"$project_dir/scripts/travel-api" profile update 1 \
  '{"displayName":"测试成员","mobile":"13800138000","alternateContact":"","emergencyContact":"","currentPassword":"","newPassword":""}' >/dev/null
"$project_dir/scripts/travel-api" trips list >/dev/null
"$project_dir/scripts/travel-api" trips create '{"id":"trip-1"}' fixed-idempotency-key >/dev/null
"$project_dir/scripts/travel-api" trips get trip-1 >/dev/null
"$project_dir/scripts/travel-api" trips update trip-1 1 '{"title":"updated"}' >/dev/null
"$project_dir/scripts/travel-api" trips overview trip-1 >/dev/null
"$project_dir/scripts/travel-api" trips backup trip-1 "$backup_file" >/dev/null
"$project_dir/scripts/travel-api" trips delete trip-1 2 >/dev/null
"$project_dir/scripts/travel-api" library state >/dev/null
"$project_dir/scripts/travel-api" library snapshot >/dev/null
"$project_dir/scripts/travel-api" library presets fixed-preset-key >/dev/null
"$project_dir/scripts/travel-api" library list members >/dev/null
"$project_dir/scripts/travel-api" library create members \
  '{"recordKey":"member-1","data":{"displayName":"测试成员","status":"active"}}' \
  fixed-library-key >/dev/null
"$project_dir/scripts/travel-api" library get members member-1 >/dev/null
"$project_dir/scripts/travel-api" library impact members member-1 >/dev/null
"$project_dir/scripts/travel-api" library update members member-1 1 \
  '{"data":{"displayName":"测试成员","status":"active"}}' >/dev/null
"$project_dir/scripts/travel-api" library delete members member-1 2 >/dev/null
"$project_dir/scripts/travel-api" library delete members member-1 2 \
  --delete-login-credential >/dev/null
"$project_dir/scripts/travel-api" records list trip-1 days >/dev/null
"$project_dir/scripts/travel-api" records create trip-1 days \
  '{"recordKey":"d1","data":{"date":"2026-10-01"}}' fixed-idempotency-key >/dev/null
"$project_dir/scripts/travel-api" records get trip-1 days record-1 >/dev/null
"$project_dir/scripts/travel-api" records impact trip-1 members record-1 >/dev/null
"$project_dir/scripts/travel-api" records update trip-1 days record-1 1 \
  '{"data":{"date":"2026-10-02"}}' >/dev/null
"$project_dir/scripts/travel-api" records delete trip-1 days record-1 2 >/dev/null
"$project_dir/scripts/travel-api" records delete trip-1 members record-1 2 \
  --cascade >/dev/null
"$project_dir/scripts/travel-api" records reorder trip-1 segments 3 \
  '{"groupKey":"all","ids":["record-1"]}' >/dev/null
"$project_dir/scripts/travel-api" audit list trip-1 >/dev/null
"$project_dir/scripts/travel-api" weather trip-1 110101 >/dev/null
"$project_dir/scripts/travel-api" map-config trip-1 >/dev/null
"$project_dir/scripts/travel-api" route trip-1 drive 104.1,30.1 104.2,30.2 >/dev/null
"$project_dir/scripts/travel-api" amap '/v3/direction/driving?origin=1,1&destination=2,2' >/dev/null
"$project_dir/scripts/travel-api" snapshots list trip-1 >/dev/null
"$project_dir/scripts/travel-api" snapshots download trip-1 snapshot-1 "$snapshot_file" >/dev/null
"$project_dir/scripts/travel-api" snapshots preview trip-1 snapshot-1 >/dev/null
"$project_dir/scripts/travel-api" snapshots restore trip-1 snapshot-1 4 --confirm --allow-delete >/dev/null
"$project_dir/scripts/travel-api" snapshot-imports preview trip-1 1 "$snapshot_file" >/dev/null
"$project_dir/scripts/travel-api" snapshot-imports apply trip-1 1 "$snapshot_file" \
  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  --allow-delete fixed-apply-key >/dev/null
"$project_dir/scripts/travel-api" trip-imports preview "$backup_file" >/dev/null
printf '{"backup":{},"confirmChecksum":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","memberMappings":[],"tagMappings":[]}\n' \
  > "$trip_import_request"
"$project_dir/scripts/travel-api" trip-imports apply "@$trip_import_request" \
  fixed-trip-import-key >/dev/null
"$project_dir/scripts/travel-api" request GET /trips/trip-1 >/dev/null
pending_warning="$(
  TRAVEL_API_MOCK_PENDING=true \
    "$project_dir/scripts/travel-api" records create trip-1 days \
      '{"recordKey":"pending","data":{"date":"2026-10-03"}}' fixed-pending-key \
      2>&1 >/dev/null
)"
pending_before_warning="$(
  TRAVEL_API_MOCK_PENDING_BEFORE=true \
    "$project_dir/scripts/travel-api" snapshots restore trip-1 snapshot-1 4 --confirm \
      2>&1 >/dev/null
)"
"$project_dir/scripts/travel-api" auth logout >/dev/null

[[ -s "$snapshot_file" ]]
[[ -s "$backup_file" ]]
[[ ! -e "$state_dir/cookies.txt" ]]
[[ ! -e "$state_dir/csrf-token" ]]
grep -Fq '数据已保存，备份文件待重试' <<<"$pending_warning"
grep -Fq '数据已保存，备份文件待重试' <<<"$pending_before_warning"

expected_paths=(
  /healthz
  /auth/bootstrap
  /auth/users
  /auth/login
  /auth/logout
  /session
  /profile
  /trips
  /trips/trip-1
  /trips/trip-1/overview
  /trips/trip-1/backup
  /library
  /library/snapshot
  /library/tags/presets
  /library/members
  /library/members/member-1
  '/library/members/member-1?deleteLoginCredential=true'
  /library/members/member-1/deletion-impact
  /trips/trip-1/days
  /trips/trip-1/days/record-1
  '/trips/trip-1/members/record-1?cascade=true'
  /trips/trip-1/members/record-1/deletion-impact
  /trips/trip-1/segments/reorder
  /trips/trip-1/audit-logs
  '/trips/trip-1/weather?adcode=110101'
  /trips/trip-1/map-config
  '/trips/trip-1/route?mode=drive&origin=104.1,30.1&destination=104.2,30.2'
  '/_AMapService/v3/direction/driving?origin=1,1&destination=2,2'
  /trips/trip-1/snapshots
  /trips/trip-1/snapshots/snapshot-1
  /trips/trip-1/snapshots/snapshot-1/restore-preview
  /trips/trip-1/snapshots/snapshot-1/restore
  /trips/trip-1/snapshot-imports/preview
  /trips/trip-1/snapshot-imports/apply
  /trip-imports/preview
  /trip-imports/apply
)
for path in "${expected_paths[@]}"; do
  grep -Fq "$path" "$mock_log" || {
    echo "travel-api 缺少接口映射：$path" >&2
    exit 1
  }
done

contract_paths=(
  '/auth/bootstrap:'
  '/auth/users:'
  '/auth/login:'
  '/auth/logout:'
  '/session:'
  '/profile:'
  '/trips:'
  '/trips/{tripId}:'
  '/trips/{tripId}/overview:'
  '/trips/{tripId}/backup:'
  '/trip-imports/preview:'
  '/trip-imports/apply:'
  '/library:'
  '/library/snapshot:'
  '/library/tags/presets:'
  '/library/{libraryResource}:'
  '/library/{libraryResource}/{libraryRecordId}:'
  '/library/{libraryResource}/{libraryRecordId}/deletion-impact:'
  '/trips/{tripId}/{resource}:'
  '/trips/{tripId}/{resource}/reorder:'
  '/trips/{tripId}/{resource}/{recordId}:'
  '/trips/{tripId}/members/{recordId}/deletion-impact:'
  '/trips/{tripId}/audit-logs:'
  '/trips/{tripId}/weather:'
  '/trips/{tripId}/map-config:'
  '/trips/{tripId}/route:'
  '/_AMapService/v3/direction/driving:'
  '/_AMapService/v3/direction/walking:'
  '/_AMapService/v3/direction/transit/integrated:'
  '/_AMapService/v5/direction/driving:'
  '/_AMapService/v5/direction/walking:'
  '/_AMapService/v3/place/text:'
  '/_AMapService/v3/geocode/regeo:'
  '/trips/{tripId}/snapshots:'
  '/trips/{tripId}/snapshots/{snapshotId}:'
  '/trips/{tripId}/snapshots/{snapshotId}/restore-preview:'
  '/trips/{tripId}/snapshots/{snapshotId}/restore:'
  '/trips/{tripId}/snapshot-imports/preview:'
  '/trips/{tripId}/snapshot-imports/apply:'
)
for path in "${contract_paths[@]}"; do
  grep -Fq "  $path" "$project_dir/api/openapi.yaml" || {
    echo "OpenAPI 缺少服务端接口：$path" >&2
    exit 1
  }
done

grep -Fq 'headers=Accept,Content-Type,X-CSRF-Token,Idempotency-Key,' "$mock_log"
grep -Fq 'body={"confirmSnapshotId":"snapshot-1","allowDelete":true}' "$mock_log"
grep -Fq 'body={"confirmSnapshotId":"snapshot-1","allowDelete":false}' "$mock_log"
grep -Fq 'insecure=true' "$mock_log"
echo "travel-api 接口映射检查通过"
