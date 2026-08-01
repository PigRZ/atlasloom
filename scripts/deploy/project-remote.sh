#!/usr/bin/env bash
set -euo pipefail

# 从已验证的确切提交滚动更新 Web/API；MongoDB 和真实攻略始终留在共享目录。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
load_project_config "$project_dir"
cd "$project_dir"

: "${DEPLOY_HOST:?请在 config/project.env 设置 DEPLOY_HOST}"
: "${DEPLOY_PROJECT:?请在 config/project.env 设置 DEPLOY_PROJECT}"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "发布已停止：请先提交或移走工作树变更。" >&2
  exit 2
fi

"$project_dir/scripts/test.sh"

deploy_user="${DEPLOY_USER:-root}"
deploy_dir="${DEPLOY_DIR:-/opt/my-travel}"
ssh_key="${SSH_KEY:-}"
deploy_platform="${DEPLOY_PLATFORM:-linux/amd64}"
git_sha="$(git rev-parse HEAD)"
release_stamp="$(date -u +%Y%m%dT%H%M%SZ)"
release_tag="$release_stamp-${git_sha:0:12}"
app_root="$deploy_dir/$DEPLOY_PROJECT"
shared_dir="$deploy_dir/shared/$DEPLOY_PROJECT"
release_dir="$app_root/releases/$release_tag"
compose_project="$DEPLOY_PROJECT"
public_base="https://$DEPLOY_HOST/${APP_BASE_PATH:-travel}"

archive="$(mktemp -t my-travel-release.XXXXXX.tar.gz)"
image_archive="$(mktemp -t my-travel-images.XXXXXX.tar.gz)"
manifest="$(mktemp -t my-travel-manifest.XXXXXX)"
runtime_patch="$(mktemp -t my-travel-runtime.XXXXXX)"
chmod 600 "$runtime_patch"
cleanup() { rm -f "$archive" "$image_archive" "$manifest" "$runtime_patch"; }
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || {
  echo "发布需要本地 Docker，以便构建确切提交对应的镜像制品。" >&2
  exit 2
}
docker buildx version >/dev/null 2>&1 || {
  echo "发布需要本地 Docker Buildx。" >&2
  exit 2
}

node_build_image="${NODE_BUILD_IMAGE:-node:24-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d}"
go_build_image="${GO_BUILD_IMAGE:-golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651}"
runtime_image="${RUNTIME_IMAGE:-alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce}"
mongo_image="${MONGO_IMAGE:-mongo:8.0-noble@sha256:277f9152905bd1f32d3ece4526e0f90906dc238f7133b24ede8446ac9740b76d}"
require_digest_pinned_image NODE_BUILD_IMAGE "$node_build_image"
require_digest_pinned_image GO_BUILD_IMAGE "$go_build_image"
require_digest_pinned_image RUNTIME_IMAGE "$runtime_image"
require_digest_pinned_image MONGO_IMAGE "$mongo_image"
RUNTIME_IMAGE="$runtime_image"
MONGO_IMAGE="$mongo_image"

git archive --format=tar.gz -o "$archive" HEAD
build_args=(
  --platform "$deploy_platform"
  --load
  --file Dockerfile
  --build-arg "APP_BASE_PATH=${APP_BASE_PATH:-travel}"
  --build-arg "NODE_BUILD_IMAGE=$node_build_image"
  --build-arg "GO_BUILD_IMAGE=$go_build_image"
  --build-arg "RUNTIME_IMAGE=$runtime_image"
  --build-arg "GO_MODULE_PROXY=${GO_MODULE_PROXY:-https://proxy.golang.org,direct}"
)
api_image="$DEPLOY_PROJECT-api:$release_tag"
web_image="$DEPLOY_PROJECT-web:$release_tag"
docker buildx build "${build_args[@]}" --target api --tag "$api_image" .
docker buildx build "${build_args[@]}" --target web --tag "$web_image" .
api_image_id="$(docker image inspect --format '{{.Id}}' "$api_image")"
web_image_id="$(docker image inspect --format '{{.Id}}' "$web_image")"
docker save "$api_image" "$web_image" | gzip -9 > "$image_archive"

{
  printf 'git_sha=%s\n' "$git_sha"
  printf 'release_tag=%s\n' "$release_tag"
  printf 'build_time=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'platform=%s\n' "$deploy_platform"
  printf 'source_archive_sha256=%s\n' "$(shasum -a 256 "$archive" | awk '{print $1}')"
  printf 'image_archive_sha256=%s\n' "$(shasum -a 256 "$image_archive" | awk '{print $1}')"
  printf 'api_image_id=%s\n' "$api_image_id"
  printf 'web_image_id=%s\n' "$web_image_id"
  printf 'node_build_image=%s\n' "$node_build_image"
  printf 'go_build_image=%s\n' "$go_build_image"
  printf 'runtime_image=%s\n' "$runtime_image"
  printf 'mongo_image=%s\n' "$mongo_image"
  printf 'web_lock_sha256=%s\n' "$(shasum -a 256 web/package-lock.json | awk '{print $1}')"
  printf 'go_sum_sha256=%s\n' "$(shasum -a 256 server/go.sum | awk '{print $1}')"
} > "$manifest"

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
  APP_BASE_PATH APP_PORT API_PORT DEPLOY_PROJECT \
  ADMIN_PROVISION_TOKEN \
  SNAPSHOT_RECONCILE_INTERVAL SNAPSHOT_RECONCILE_TIMEOUT SNAPSHOT_RECONCILE_BATCH \
  AMAP_WEBSERVICE_KEY AMAP_JSAPI_KEY AMAP_JSAPI_SECURITY_CODE \
  MONGO_IMAGE RUNTIME_IMAGE; do
  append_runtime_value "$key"
done

ssh_options=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
if [[ -n "$ssh_key" ]]; then
  ssh_options+=(-o IdentitiesOnly=yes -i "$ssh_key")
fi
remote="$deploy_user@$DEPLOY_HOST"

ssh "${ssh_options[@]}" "$remote" \
  "test -f '$shared_dir/runtime.env' &&
   test -d '$shared_dir/mongo' &&
   test -d '$shared_dir/snapshots' &&
   docker version >/dev/null &&
   docker compose version >/dev/null"

scp "${ssh_options[@]}" "$archive" "$remote:/tmp/my-travel-$release_tag.tar.gz"
scp "${ssh_options[@]}" "$image_archive" "$remote:/tmp/my-travel-$release_tag.images.tar.gz"
scp "${ssh_options[@]}" "$manifest" "$remote:/tmp/my-travel-$release_tag.manifest"
scp "${ssh_options[@]}" "$runtime_patch" "$remote:/tmp/my-travel-$release_tag.runtime"

ssh "${ssh_options[@]}" "$remote" bash -s -- \
  "$app_root" "$release_dir" "$shared_dir" "$compose_project" "$release_tag" "$public_base" <<'REMOTE'
set -euo pipefail

app_root="$1"
release_dir="$2"
shared_dir="$3"
compose_project="$4"
release_tag="$5"
public_base="$6"
archive="/tmp/my-travel-$release_tag.tar.gz"
image_archive="/tmp/my-travel-$release_tag.images.tar.gz"
manifest="/tmp/my-travel-$release_tag.manifest"
runtime_patch="/tmp/my-travel-$release_tag.runtime"
runtime_env="$shared_dir/runtime.env"
runtime_before="$shared_dir/runtime.env.before-$release_tag"
lock_file="$shared_dir/deploy.lock"

mkdir -p "$shared_dir" "$app_root/releases"
exec 9>"$lock_file"
flock -n 9 || {
  echo "已有发布任务正在执行。" >&2
  exit 2
}

previous_release="$(readlink -f "$app_root/current" 2>/dev/null || true)"
updated=0

cleanup_remote() {
  rm -f "$archive" "$image_archive" "$manifest" "$runtime_patch"
}

rollback() {
  local exit_code=$?
  if ((exit_code != 0)) && [[ -f "$runtime_before" ]]; then
    cp "$runtime_before" "$runtime_env"
    chmod 600 "$runtime_env"
  fi
  if ((exit_code != 0 && updated == 1)) && [[ -n "$previous_release" && -f "$previous_release/compose.yaml" ]]; then
    echo "候选 release 验证失败，正在恢复上一 release。" >&2
    (
      cd "$previous_release"
      previous_tag="$(sed -n 's/^release_tag=//p' BUILD-MANIFEST 2>/dev/null | tail -1)"
      if [[ -n "$previous_tag" ]]; then
        export RELEASE_IMAGE_TAG="$previous_tag"
      fi
      docker compose -p "$compose_project" --env-file "$runtime_env" -f compose.yaml \
        up -d --no-build --no-deps api web
    ) || true
  fi
  rm -f "$runtime_before"
  cleanup_remote
  exit "$exit_code"
}
trap rollback EXIT

cp "$runtime_env" "$runtime_before"
chmod 600 "$runtime_before"

manifest_value() {
  sed -n "s/^$1=//p" "$manifest" | tail -1
}

verify_sha256() {
  local file="$1"
  local expected="$2"
  local actual
  actual="$(sha256sum "$file" | awk '{print $1}')"
  if [[ -z "$expected" || "$actual" != "$expected" ]]; then
    echo "制品摘要校验失败：$file" >&2
    exit 2
  fi
}

verify_sha256 "$archive" "$(manifest_value source_archive_sha256)"
verify_sha256 "$image_archive" "$(manifest_value image_archive_sha256)"

set_runtime_value() {
  local key="$1"
  local value="$2"
  local temp found=0 line
  temp="$(mktemp "$shared_dir/runtime.XXXXXX")"
  chmod 600 "$temp"
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == "$key="* ]]; then
      printf '%s=%s\n' "$key" "$value" >> "$temp"
      found=1
    else
      printf '%s\n' "$line" >> "$temp"
    fi
  done < "$runtime_env"
  if ((found == 0)); then
    printf '%s=%s\n' "$key" "$value" >> "$temp"
  fi
  mv "$temp" "$runtime_env"
}

while IFS='=' read -r key value || [[ -n "$key" ]]; do
  [[ -n "$key" ]] || continue
  set_runtime_value "$key" "$value"
done < "$runtime_patch"
# 公网仅允许 HTTPS 会话；本地调试开关不能传播到服务器。
set_runtime_value COOKIE_SECURE true

mkdir -p "$release_dir/.local/docker"
tar -xzf "$archive" -C "$release_dir"
install -m 600 "$manifest" "$release_dir/BUILD-MANIFEST"
ln -sfn "$shared_dir/mongo" "$release_dir/.local/docker/mongo"
ln -sfn "$shared_dir/snapshots" "$release_dir/.local/docker/snapshots"

read_env() {
  sed -n "s/^$1=//p" "$runtime_env" | tail -1
}
app_port="$(read_env APP_PORT)"
api_port="$(read_env API_PORT)"
: "${app_port:?runtime.env 缺少 APP_PORT}"
: "${api_port:?runtime.env 缺少 API_PORT}"

cd "$release_dir"
export RELEASE_IMAGE_TAG="$release_tag"
compose=(docker compose -p "$compose_project" --env-file "$runtime_env" -f compose.yaml)

# 远端只加载本地验证过的确切镜像制品，不从可变源码或基础镜像重新构建。
gzip -dc "$image_archive" | docker load >/dev/null
api_image="$compose_project-api:$release_tag"
web_image="$compose_project-web:$release_tag"
actual_api_image_id="$(docker image inspect --format '{{.Id}}' "$api_image")"
actual_web_image_id="$(docker image inspect --format '{{.Id}}' "$web_image")"
if [[ "$actual_api_image_id" != "$(manifest_value api_image_id)" ||
      "$actual_web_image_id" != "$(manifest_value web_image_id)" ]]; then
  echo "加载后的应用镜像与本地构建清单不一致。" >&2
  exit 2
fi
# 远端 Bash 脚本本身经 SSH stdin 传入；一次性容器不得继续读取同一输入流。
"${compose[@]}" run --rm --no-deps -T snapshot-init </dev/null

mongo_container="$("${compose[@]}" ps -q mongo)"
if [[ -z "$mongo_container" ]]; then
  echo "未发现现有数据库容器，正在建立空数据库环境。"
  "${compose[@]}" up -d mongo
  mongo_container="$("${compose[@]}" ps -q mongo)"
  [[ -n "$mongo_container" ]] || {
    echo "MongoDB 容器启动失败。" >&2
    exit 2
  }
  for attempt in $(seq 1 40); do
    health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$mongo_container")"
    [[ "$health" == "healthy" ]] && break
    sleep 2
  done
  [[ "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$mongo_container")" == "healthy" ]] || {
    echo "MongoDB 健康检查未通过。" >&2
    exit 2
  }
  "${compose[@]}" up -d mongo-init
  init_container="$("${compose[@]}" ps -a -q mongo-init)"
  [[ -n "$init_container" ]] || {
    echo "MongoDB 副本集初始化容器未创建。" >&2
    exit 2
  }
  for attempt in $(seq 1 40); do
    init_status="$(docker inspect -f '{{.State.Status}}' "$init_container")"
    [[ "$init_status" == "exited" ]] && break
    sleep 1
  done
  [[ "$(docker inspect -f '{{.State.ExitCode}}' "$init_container")" == "0" ]] || {
    echo "MongoDB 副本集初始化失败。" >&2
    exit 2
  }
else
  data_network="${compose_project}_data"
  docker network inspect "$data_network" >/dev/null 2>&1 || {
    echo "数据库容器不符合当前 Compose 数据网络定义，发布已停止。" >&2
    exit 2
  }
  mongo_name="$(docker inspect -f '{{.Name}}' "$mongo_container" | sed 's#^/##')"
  docker network inspect "$data_network" \
    --format '{{range .Containers}}{{.Name}}{{"\n"}}{{end}}' |
    grep -Fxq "$mongo_name" || {
      echo "数据库容器未加入当前 Compose 数据网络，发布已停止。" >&2
      exit 2
    }
fi
docker update --restart unless-stopped "$mongo_container" >/dev/null

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

# 只滚动应用容器；MongoDB 和副本集初始化容器不参与发布切换。
updated=1
"${compose[@]}" up -d --no-build --no-deps api
wait_for_health "http://127.0.0.1:$api_port/healthz"
"${compose[@]}" up -d --no-build --no-deps web
wait_for_health "http://127.0.0.1:$app_port/healthz"

wait_for_health "$public_base/healthz"
wait_for_health "$public_base/api/healthz"

ln -sfn "$release_dir" "$app_root/current"
updated=0
trap - EXIT
rm -f "$runtime_before"
cleanup_remote

printf 'deployed_sha=%s\n' "$(sed -n 's/^git_sha=//p' "$release_dir/BUILD-MANIFEST")"
printf 'release=%s\n' "$release_dir"
REMOTE

echo "服务器发布完成。"
