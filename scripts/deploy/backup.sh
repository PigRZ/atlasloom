#!/usr/bin/env bash
set -euo pipefail

# 把远程数据库、快照、运行配置和网关配置下载到本机受限目录，供重建前恢复。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
load_project_config "$project_dir"

adopt_runtime=false
if [[ "${1:-}" == "--adopt-runtime" ]]; then
  adopt_runtime=true
  shift
fi
(($# == 0)) || {
  echo "用法：./scripts/deploy/backup.sh [--adopt-runtime]" >&2
  exit 2
}

: "${DEPLOY_HOST:?请在 config/project.env 设置 DEPLOY_HOST}"
: "${DEPLOY_PROJECT:?请在 config/project.env 设置 DEPLOY_PROJECT}"

deploy_user="${DEPLOY_USER:-root}"
deploy_dir="${DEPLOY_DIR:-/opt/my-travel}"
ssh_key="${SSH_KEY:-}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="$project_dir/.local/server-backups/$DEPLOY_PROJECT/$stamp"
mkdir -p "$backup_dir"
chmod 700 "$project_dir/.local/server-backups" \
  "$project_dir/.local/server-backups/$DEPLOY_PROJECT" "$backup_dir"

ssh_options=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
if [[ -n "$ssh_key" ]]; then
  ssh_options+=(-o IdentitiesOnly=yes -i "$ssh_key")
fi
remote="$deploy_user@$DEPLOY_HOST"
shared_dir="$deploy_dir/shared/$DEPLOY_PROJECT"
app_root="$deploy_dir/$DEPLOY_PROJECT"

ssh "${ssh_options[@]}" "$remote" \
  "test -f '$shared_dir/runtime.env' &&
   test -d '$shared_dir/snapshots' &&
   test -f '$app_root/current/compose.yaml'"

scp "${ssh_options[@]}" "$remote:$shared_dir/runtime.env" "$backup_dir/runtime.env" >/dev/null
chmod 600 "$backup_dir/runtime.env"

ssh "${ssh_options[@]}" "$remote" bash -s -- "$shared_dir" "$app_root" "$DEPLOY_PROJECT" <<'REMOTE' \
  > "$backup_dir/mongo.archive.gz"
set -euo pipefail
shared_dir="$1"
app_root="$2"
project="$3"
runtime_env="$shared_dir/runtime.env"
database="$(sed -n 's/^MONGODB_DATABASE=//p' "$runtime_env" | tail -1)"
container="$(docker compose -p "$project" --env-file "$runtime_env" \
  -f "$app_root/current/compose.yaml" ps -q mongo)"
: "${database:?运行配置缺少 MONGODB_DATABASE}"
: "${container:?找不到 MongoDB 容器}"
docker exec "$container" mongodump --quiet --archive --gzip --db "$database"
REMOTE
chmod 600 "$backup_dir/mongo.archive.gz"
gzip -t "$backup_dir/mongo.archive.gz"

ssh "${ssh_options[@]}" "$remote" \
  "tar -czf - -C '$shared_dir' snapshots" > "$backup_dir/snapshots.tar.gz"
chmod 600 "$backup_dir/snapshots.tar.gz"
tar -tzf "$backup_dir/snapshots.tar.gz" >/dev/null

scp "${ssh_options[@]}" \
  "$remote:$app_root/current/BUILD-MANIFEST" "$backup_dir/BUILD-MANIFEST" >/dev/null
chmod 600 "$backup_dir/BUILD-MANIFEST"

ssh "${ssh_options[@]}" "$remote" bash -s -- "$DEPLOY_PROJECT" <<'REMOTE' \
  > "$backup_dir/nginx.tar.gz"
set -euo pipefail
project="$1"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
mkdir -p "$temporary/conf.d" "$temporary/personal-gateway.d"
for file in \
  "/etc/nginx/conf.d/$project.conf" \
  /etc/nginx/conf.d/personal-gateway.conf \
  /etc/nginx/conf.d/personal-gateway-https.conf \
  "/etc/nginx/personal-gateway.d/$project.conf"; do
  [[ -f "$file" ]] || continue
  case "$file" in
    /etc/nginx/conf.d/*) cp "$file" "$temporary/conf.d/" ;;
    /etc/nginx/personal-gateway.d/*) cp "$file" "$temporary/personal-gateway.d/" ;;
  esac
done
tar -czf - -C "$temporary" .
REMOTE
chmod 600 "$backup_dir/nginx.tar.gz"

(
  cd "$backup_dir"
  shasum -a 256 runtime.env mongo.archive.gz snapshots.tar.gz BUILD-MANIFEST nginx.tar.gz \
    > CHECKSUMS
)
chmod 600 "$backup_dir/CHECKSUMS"

if [[ "$adopt_runtime" == "true" ]]; then
  config_file="$project_dir/config/project.env"
  [[ -f "$config_file" ]] || {
    cp "$project_dir/config/project.env.example" "$config_file"
    chmod 600 "$config_file"
  }
  imported=0
  for key in \
    APP_BASE_PATH APP_PORT API_PORT MONGODB_DATABASE \
    BOOTSTRAP_TOKEN ADMIN_PROVISION_TOKEN \
    AMAP_WEBSERVICE_KEY AMAP_JSAPI_KEY AMAP_JSAPI_SECURITY_CODE \
    MONGO_IMAGE NODE_BUILD_IMAGE GO_BUILD_IMAGE RUNTIME_IMAGE GO_MODULE_PROXY; do
    value="$(sed -n "s/^$key=//p" "$backup_dir/runtime.env" | tail -1)"
    [[ -n "$value" ]] || continue
    set_env_file_value "$config_file" "$key" "$value"
    imported=$((imported + 1))
  done
  chmod 600 "$config_file"
  printf '已把 %d 项服务器运行配置接回本机受限配置文件。\n' "$imported"
fi

printf '服务器恢复包已保存：%s\n' "$backup_dir"
