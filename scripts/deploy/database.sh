#!/usr/bin/env bash
set -euo pipefail

# 把本机受限恢复包中的完整数据库与快照恢复到已部署的远程项目。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
load_project_config "$project_dir"

backup_dir="${1:-}"
[[ -n "$backup_dir" && -d "$backup_dir" ]] || {
  echo "用法：./scripts/deploy/database.sh <本地恢复包目录>" >&2
  exit 2
}
backup_dir="$(cd "$backup_dir" && pwd)"
(
  cd "$backup_dir"
  shasum -a 256 -c CHECKSUMS >/dev/null
) || {
  echo "恢复包校验失败，禁止覆盖服务器数据库。" >&2
  exit 2
}

: "${DEPLOY_HOST:?请在 config/project.env 设置 DEPLOY_HOST}"
: "${DEPLOY_PROJECT:?请在 config/project.env 设置 DEPLOY_PROJECT}"
if [[ -n "$(git -C "$project_dir" status --porcelain)" ]]; then
  echo "数据库恢复已停止：请先提交或移走工作树变更。" >&2
  exit 2
fi
[[ -t 0 ]] || {
  echo "数据库恢复需要交互终端确认。" >&2
  exit 2
}
read -r -p "输入 RESTORE-$DEPLOY_PROJECT 确认以本地恢复包覆盖服务器数据库：" confirmation
[[ "$confirmation" == "RESTORE-$DEPLOY_PROJECT" ]] || {
  echo "已取消。" >&2
  exit 2
}

deploy_user="${DEPLOY_USER:-root}"
deploy_dir="${DEPLOY_DIR:-/opt/my-travel}"
ssh_key="${SSH_KEY:-}"
ssh_options=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
if [[ -n "$ssh_key" ]]; then
  ssh_options+=(-o IdentitiesOnly=yes -i "$ssh_key")
fi
remote="$deploy_user@$DEPLOY_HOST"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
target_sha="$(git -C "$project_dir" rev-parse HEAD)"
public_base="https://$DEPLOY_HOST/${APP_BASE_PATH:-travel}"
remote_dump="/tmp/$DEPLOY_PROJECT-mongo-$stamp.archive.gz"
remote_snapshots="/tmp/$DEPLOY_PROJECT-snapshots-$stamp.tar.gz"

scp "${ssh_options[@]}" "$backup_dir/mongo.archive.gz" "$remote:$remote_dump" >/dev/null
scp "${ssh_options[@]}" "$backup_dir/snapshots.tar.gz" "$remote:$remote_snapshots" >/dev/null

ssh "${ssh_options[@]}" "$remote" bash -s -- \
  "$deploy_dir" "$DEPLOY_PROJECT" "$remote_dump" "$remote_snapshots" \
  "$target_sha" "$public_base" <<'REMOTE'
set -euo pipefail
deploy_dir="$1"
project="$2"
dump="$3"
snapshots_archive="$4"
target_sha="$5"
public_base="$6"
app_root="$deploy_dir/$project"
shared_dir="$deploy_dir/shared/$project"
runtime_env="$shared_dir/runtime.env"
current="$app_root/current"
previous_release="$(readlink -f "$current" 2>/dev/null || true)"
lock_file="$shared_dir/deploy.lock"
before_dump="/tmp/$project-before-${target_sha:0:12}.archive.gz"
snapshot_before="$shared_dir/snapshots.before-${target_sha:0:12}"

cleanup_uploads() {
  rm -f "$dump" "$snapshots_archive"
}
trap cleanup_uploads EXIT

[[ -f "$runtime_env" && -n "$previous_release" &&
  -f "$previous_release/compose.yaml" ]] || {
  echo "服务器项目尚未完成环境构建和代码部署。" >&2
  exit 2
}

exec 9>"$lock_file"
flock -n 9 || {
  echo "已有发布任务正在执行。" >&2
  exit 2
}

target_release=""
for candidate in "$app_root"/releases/*; do
  [[ -f "$candidate/BUILD-MANIFEST" ]] || continue
  candidate_sha="$(sed -n 's/^git_sha=//p' "$candidate/BUILD-MANIFEST" | tail -1)"
  if [[ "$candidate_sha" == "$target_sha" ]]; then
    target_release="$candidate"
  fi
done
[[ -n "$target_release" && -f "$target_release/compose.yaml" ]] || {
  echo "服务器没有与本地提交 $target_sha 对应的候选 release；请先执行项目部署。" >&2
  exit 2
}

previous_tag="$(sed -n 's/^release_tag=//p' "$previous_release/BUILD-MANIFEST" | tail -1)"
target_tag="$(sed -n 's/^release_tag=//p' "$target_release/BUILD-MANIFEST" | tail -1)"
: "${previous_tag:?旧 release 缺少 release_tag}"
: "${target_tag:?候选 release 缺少 release_tag}"
docker image inspect "$project-api:$target_tag" "$project-web:$target_tag" >/dev/null

previous_compose=(
  docker compose -p "$project" --env-file "$runtime_env"
  -f "$previous_release/compose.yaml"
)
target_compose=(
  docker compose -p "$project" --env-file "$runtime_env"
  -f "$target_release/compose.yaml"
)
mongo_container="$("${previous_compose[@]}" ps -q mongo)"
: "${mongo_container:?找不到 MongoDB 容器}"
database="$(sed -n 's/^MONGODB_DATABASE=//p' "$runtime_env" | tail -1)"
: "${database:?运行配置缺少 MONGODB_DATABASE}"

cleanup() {
  rm -f "$dump" "$snapshots_archive" "$before_dump"
}

database_changed=0
snapshots_changed=0
applications_stopped=0
rollback() {
  exit_code=$?
  trap - EXIT
  set +e
  if ((database_changed == 1)); then
    docker exec -e "RESTORE_DATABASE=$database" "$mongo_container" \
      mongosh --quiet --eval \
      'db.getSiblingDB(process.env.RESTORE_DATABASE).dropDatabase();' >/dev/null
    docker exec -i "$mongo_container" \
      mongorestore --quiet --archive --gzip < "$before_dump"
  fi
  if ((snapshots_changed == 1)); then
    rm -rf "$shared_dir/snapshots"
    mv "$snapshot_before" "$shared_dir/snapshots"
  fi
  ln -sfn "$previous_release" "$current"
  if ((applications_stopped == 1)); then
    (
      cd "$previous_release"
      export RELEASE_IMAGE_TAG="$previous_tag"
      "${previous_compose[@]}" up -d --no-build api web >/dev/null
    ) || true
  fi
  cleanup
  echo "数据库切换失败，已恢复发布前数据库、快照和 release。" >&2
  exit "$exit_code"
}
trap rollback EXIT

rm -f "$before_dump"
docker exec "$mongo_container" \
  mongodump --quiet --archive --gzip --db "$database" > "$before_dump"
test -s "$before_dump"

"${previous_compose[@]}" stop api web >/dev/null
applications_stopped=1
docker exec -e "RESTORE_DATABASE=$database" "$mongo_container" \
  mongosh --quiet --eval \
  'db.getSiblingDB(process.env.RESTORE_DATABASE).dropDatabase();' >/dev/null
database_changed=1
docker exec -i "$mongo_container" \
  mongorestore --quiet --archive --gzip < "$dump"

rm -rf "$snapshot_before"
if [[ -d "$shared_dir/snapshots" ]]; then
  mv "$shared_dir/snapshots" "$snapshot_before"
fi
snapshots_changed=1
tar -xzf "$snapshots_archive" -C "$shared_dir"

cd "$target_release"
export RELEASE_IMAGE_TAG="$target_tag"
"${target_compose[@]}" run --rm --no-deps -T snapshot-init </dev/null
"${target_compose[@]}" up -d --no-build api web >/dev/null

wait_for_health() {
  local url="$1"
  local attempt
  for attempt in $(seq 1 40); do
    if curl --fail --silent "$url" >/dev/null; then
      return 0
    fi
    sleep 2
  done
  curl --fail --silent --show-error "$url" >/dev/null
}
app_port="$(sed -n 's/^APP_PORT=//p' "$runtime_env" | tail -1)"
api_port="$(sed -n 's/^API_PORT=//p' "$runtime_env" | tail -1)"
wait_for_health "http://127.0.0.1:$api_port/healthz"
wait_for_health "http://127.0.0.1:$app_port/healthz"
wait_for_health "$public_base/healthz"
wait_for_health "$public_base/api/healthz"

ln -sfn "$target_release" "$current"
trap - EXIT
rm -rf "$snapshot_before"
cleanup
printf 'deployed_sha=%s\n' "$target_sha"
printf '服务器数据库和快照已从本地恢复包恢复。\n'
REMOTE
