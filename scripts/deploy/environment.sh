#!/usr/bin/env bash
set -euo pipefail

# 第一层：幂等安装服务器依赖、运行目录、受限配置和 Nginx；可选可恢复地重建项目目录。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
load_project_config "$project_dir"

reset_project=false
backup_dir=""
while (($# > 0)); do
  case "$1" in
    --reset-project)
      reset_project=true
      shift
      ;;
    --backup-dir)
      (($# >= 2)) || {
        echo "--backup-dir 需要本地恢复包目录。" >&2
        exit 2
      }
      backup_dir="$2"
      shift 2
      ;;
    *)
      echo "用法：./scripts/deploy/environment.sh [--reset-project --backup-dir <本地恢复包>]" >&2
      exit 2
      ;;
  esac
done

: "${DEPLOY_HOST:?请在 config/project.env 设置 DEPLOY_HOST}"
: "${DEPLOY_PROJECT:?请在 config/project.env 设置 DEPLOY_PROJECT}"
: "${APP_BASE_PATH:?请在 config/project.env 设置 APP_BASE_PATH}"
: "${APP_PORT:?请在 config/project.env 设置 APP_PORT}"
: "${API_PORT:?请在 config/project.env 设置 API_PORT}"
: "${MONGODB_DATABASE:?请在 config/project.env 设置 MONGODB_DATABASE}"

[[ "$DEPLOY_PROJECT" =~ ^[a-z0-9][a-z0-9-]{0,62}$ ]] || {
  echo "DEPLOY_PROJECT 只能使用小写字母、数字和连字符。" >&2
  exit 2
}
[[ "$APP_BASE_PATH" =~ ^[A-Za-z0-9_-]+$ ]] || {
  echo "APP_BASE_PATH 只能使用字母、数字、下划线和连字符。" >&2
  exit 2
}
[[ "$APP_PORT" =~ ^[0-9]+$ && "$API_PORT" =~ ^[0-9]+$ ]] || {
  echo "APP_PORT 和 API_PORT 必须是数字。" >&2
  exit 2
}

config_file="$project_dir/config/project.env"
if [[ -z "${BOOTSTRAP_TOKEN:-}" ]]; then
  BOOTSTRAP_TOKEN="$(openssl rand -hex 32)"
  set_env_file_value "$config_file" BOOTSTRAP_TOKEN "$BOOTSTRAP_TOKEN"
  chmod 600 "$config_file"
  export BOOTSTRAP_TOKEN
  echo "已在本机受限配置中生成数据库初始化令牌。"
fi

if [[ "$reset_project" == "true" ]]; then
  [[ -n "$backup_dir" && -d "$backup_dir" ]] || {
    echo "重建项目前必须通过 --backup-dir 提供已下载的恢复包。" >&2
    exit 2
  }
  (
    cd "$backup_dir"
    shasum -a 256 -c CHECKSUMS >/dev/null
  ) || {
    echo "恢复包校验失败，禁止重建服务器项目。" >&2
    exit 2
  }
  [[ -t 0 ]] || {
    echo "重建项目需要交互终端确认。" >&2
    exit 2
  }
  read -r -p "输入 RESET-$DEPLOY_PROJECT 确认归档旧项目并建立空环境：" confirmation
  [[ "$confirmation" == "RESET-$DEPLOY_PROJECT" ]] || {
    echo "已取消。" >&2
    exit 2
  }
fi

deploy_user="${DEPLOY_USER:-root}"
deploy_dir="${DEPLOY_DIR:-/opt/my-travel}"
ssh_key="${SSH_KEY:-}"
nginx_mode="${NGINX_MODE:-standalone}"
[[ "$nginx_mode" == "standalone" || "$nginx_mode" == "shared" ]] || {
  echo "NGINX_MODE 只能是 standalone 或 shared。" >&2
  exit 2
}

runtime_patch="$(mktemp -t my-travel-provision-runtime.XXXXXX)"
nginx_candidate="$(mktemp -t my-travel-nginx.XXXXXX)"
chmod 600 "$runtime_patch" "$nginx_candidate"
cleanup() { rm -f "$runtime_patch" "$nginx_candidate"; }
trap cleanup EXIT

append_runtime_value() {
  local key="$1"
  local value="${!key-}"
  [[ -n "$value" ]] || return 0
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || {
    echo "$key 不能包含换行。" >&2
    exit 2
  }
  printf '%s=%s\n' "$key" "$value" >> "$runtime_patch"
}

for key in \
  APP_BASE_PATH APP_PORT API_PORT MONGODB_DATABASE DEPLOY_PROJECT \
  BOOTSTRAP_TOKEN ADMIN_PROVISION_TOKEN \
  SNAPSHOT_RECONCILE_INTERVAL SNAPSHOT_RECONCILE_TIMEOUT SNAPSHOT_RECONCILE_BATCH \
  AMAP_WEBSERVICE_KEY AMAP_JSAPI_KEY AMAP_JSAPI_SECURITY_CODE \
  MONGO_IMAGE NODE_BUILD_IMAGE GO_BUILD_IMAGE RUNTIME_IMAGE GO_MODULE_PROXY; do
  append_runtime_value "$key"
done

render_template() {
  local template="$1"
  sed \
    -e "s#__SERVER_IP__#$DEPLOY_HOST#g" \
    -e "s#__APP_BASE_PATH__#$APP_BASE_PATH#g" \
    -e "s#__APP_PORT__#$APP_PORT#g" \
    -e "s#__API_PORT__#$API_PORT#g" \
    -e "s#__IP_CERTIFICATE_FULLCHAIN__#${TLS_CERTIFICATE_FULLCHAIN:-}#g" \
    -e "s#__IP_CERTIFICATE_PRIVATE_KEY__#${TLS_CERTIFICATE_PRIVATE_KEY:-}#g" \
    "$template"
}

if [[ "$nginx_mode" == "standalone" ]]; then
  : "${TLS_CERTIFICATE_FULLCHAIN:?standalone 模式需要 TLS_CERTIFICATE_FULLCHAIN}"
  : "${TLS_CERTIFICATE_PRIVATE_KEY:?standalone 模式需要 TLS_CERTIFICATE_PRIVATE_KEY}"
  render_template "$project_dir/deploy/nginx.conf.example" > "$nginx_candidate"
else
  render_template "$project_dir/deploy/nginx-location.conf.example" > "$nginx_candidate"
fi
grep -Eq '__[A-Z_]+__' "$nginx_candidate" && {
  echo "Nginx 候选配置仍有未替换占位符。" >&2
  exit 2
}

ssh_options=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
if [[ -n "$ssh_key" ]]; then
  ssh_options+=(-o IdentitiesOnly=yes -i "$ssh_key")
fi
remote="$deploy_user@$DEPLOY_HOST"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"

scp "${ssh_options[@]}" "$runtime_patch" "$remote:/tmp/$DEPLOY_PROJECT-runtime-$stamp.env" >/dev/null
scp "${ssh_options[@]}" "$nginx_candidate" "$remote:/tmp/$DEPLOY_PROJECT-nginx-$stamp.conf" >/dev/null

ssh "${ssh_options[@]}" "$remote" bash -s -- \
  "$deploy_dir" "$DEPLOY_PROJECT" "$nginx_mode" "$stamp" "$reset_project" <<'REMOTE'
set -euo pipefail

deploy_dir="$1"
project="$2"
nginx_mode="$3"
stamp="$4"
reset_project="$5"
app_root="$deploy_dir/$project"
shared_dir="$deploy_dir/shared/$project"
runtime_patch="/tmp/$project-runtime-$stamp.env"
nginx_candidate="/tmp/$project-nginx-$stamp.conf"

[[ "$deploy_dir" == /opt/* || "$deploy_dir" == /srv/* ]] || {
  echo "DEPLOY_DIR 必须位于 /opt 或 /srv 下。" >&2
  exit 2
}
[[ "$project" =~ ^[a-z0-9][a-z0-9-]{0,62}$ ]] || exit 2

cleanup() { rm -f "$runtime_patch" "$nginx_candidate"; }
trap cleanup EXIT

install_dependencies() {
  if command -v docker >/dev/null 2>&1 &&
    docker compose version >/dev/null 2>&1 &&
    command -v nginx >/dev/null 2>&1 &&
    command -v curl >/dev/null 2>&1; then
    return
  fi
  [[ "$(id -u)" == "0" ]] || {
    echo "首次构建服务器环境需要 root 权限。" >&2
    exit 2
  }
  if command -v dnf >/dev/null 2>&1; then
    dnf install -y docker nginx curl ca-certificates tar util-linux
  elif command -v yum >/dev/null 2>&1; then
    yum install -y docker nginx curl ca-certificates tar util-linux
  elif command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
      docker.io docker-compose-v2 nginx curl ca-certificates tar util-linux
  else
    echo "暂不支持该服务器的软件包管理器。" >&2
    exit 2
  fi
  systemctl enable --now docker nginx
  docker compose version >/dev/null 2>&1 || {
    echo "Docker Compose 插件安装失败，请先完成系统级安装。" >&2
    exit 2
  }
}

install_dependencies

if [[ "$reset_project" == "true" ]]; then
  recovery_root="$deploy_dir/recovery/$project/$stamp"
  mkdir -p "$recovery_root"
  if [[ -f "$app_root/current/compose.yaml" && -f "$shared_dir/runtime.env" ]]; then
    (
      cd "$app_root/current"
      docker compose -p "$project" --env-file "$shared_dir/runtime.env" \
        -f compose.yaml down --remove-orphans
    )
  fi
  [[ ! -e "$app_root" ]] || mv "$app_root" "$recovery_root/app"
  [[ ! -e "$shared_dir" ]] || mv "$shared_dir" "$recovery_root/shared"
  printf '旧项目已归档：%s\n' "$recovery_root"
fi

mkdir -p "$app_root/releases" "$shared_dir/mongo" "$shared_dir/snapshots"
chmod 700 "$shared_dir" "$shared_dir/snapshots"
runtime_env="$shared_dir/runtime.env"
if [[ ! -f "$runtime_env" ]]; then
  : > "$runtime_env"
fi
chmod 600 "$runtime_env"

set_runtime_value() {
  local key="$1"
  local value="$2"
  local temporary found=0 line
  temporary="$(mktemp "$shared_dir/runtime.XXXXXX")"
  chmod 600 "$temporary"
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == "$key="* ]]; then
      printf '%s=%s\n' "$key" "$value" >> "$temporary"
      found=1
    else
      printf '%s\n' "$line" >> "$temporary"
    fi
  done < "$runtime_env"
  if ((found == 0)); then
    printf '%s=%s\n' "$key" "$value" >> "$temporary"
  fi
  mv "$temporary" "$runtime_env"
}

while IFS='=' read -r key value || [[ -n "$key" ]]; do
  [[ -n "$key" ]] || continue
  set_runtime_value "$key" "$value"
done < "$runtime_patch"
set_runtime_value COOKIE_SECURE true

if [[ "$nginx_mode" == "shared" ]]; then
  [[ -d /etc/nginx/personal-gateway.d ]] || {
    echo "shared 模式要求现有网关包含 /etc/nginx/personal-gateway.d。" >&2
    exit 2
  }
  nginx_target="/etc/nginx/personal-gateway.d/$project.conf"
else
  nginx_target="/etc/nginx/conf.d/$project.conf"
fi
nginx_before=""
if [[ -f "$nginx_target" ]]; then
  nginx_before="$nginx_target.before-$stamp"
  cp "$nginx_target" "$nginx_before"
fi
install -m 644 "$nginx_candidate" "$nginx_target"
if ! nginx -t; then
  if [[ -n "$nginx_before" ]]; then
    mv "$nginx_before" "$nginx_target"
  else
    rm -f "$nginx_target"
  fi
  nginx -t || true
  echo "Nginx 候选配置无效，已恢复原配置。" >&2
  exit 2
fi
rm -f "$nginx_before"
systemctl enable nginx >/dev/null
systemctl reload nginx

docker version >/dev/null
docker compose version >/dev/null
printf '服务器环境构建完成：%s\n' "$shared_dir"
REMOTE
