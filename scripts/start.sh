#!/usr/bin/env bash
set -euo pipefail

# 生成本机受限运行配置，并只在回环端口启动当前 Compose 栈。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
load_project_config "$project_dir"
cd "$project_dir"

command -v docker >/dev/null 2>&1 || {
  echo "未找到 Docker。请安装并启动 Docker Desktop 后再运行。" >&2
  exit 2
}
docker compose version >/dev/null 2>&1 || {
  echo "Docker Compose 插件不可用。" >&2
  exit 2
}
docker buildx version >/dev/null 2>&1 || {
  echo "Docker Buildx 插件不可用。" >&2
  exit 2
}

state_dir="$project_dir/.local/app"
runtime_env="$state_dir/runtime.env"
mkdir -p "$state_dir" "$project_dir/.local/docker/mongo" "$project_dir/.local/docker/snapshots"
chmod 700 "$state_dir"
if [[ ! -f "$runtime_env" ]]; then
  bootstrap_token="$(openssl rand -hex 32)"
  printf 'BOOTSTRAP_TOKEN=%s\n' "$bootstrap_token" > "$runtime_env"
  chmod 600 "$runtime_env"
  echo "已创建仅本机使用的 bootstrap token：$runtime_env" >&2
fi

set_env_file_value "$runtime_env" APP_BASE_PATH "${APP_BASE_PATH:-travel}"
set_env_file_value "$runtime_env" APP_PORT "${APP_PORT:-8501}"
set_env_file_value "$runtime_env" API_PORT "${API_PORT:-8502}"
set_env_file_value "$runtime_env" MONGODB_DATABASE "${MONGODB_DATABASE:-my_travel}"
set_env_file_value "$runtime_env" COOKIE_SECURE "${COOKIE_SECURE:-false}"

# Compose 5 会自动把 build 转交给 Bake；Docker Desktop 某些版本的 Bake
# 会话可能在含非 ASCII 路径的工作区失效。显式 Buildx 后再由 Compose 启动，
# 镜像定义和运行拓扑仍只来自 Dockerfile 与 compose.yaml。
image_prefix="${DEPLOY_PROJECT:-my-travel}"
image_tag="${RELEASE_IMAGE_TAG:-local}"
node_build_image="${NODE_BUILD_IMAGE:-node:24-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d}"
go_build_image="${GO_BUILD_IMAGE:-golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651}"
runtime_image="${RUNTIME_IMAGE:-alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce}"
mongo_image="${MONGO_IMAGE:-mongo:8.0-noble@sha256:277f9152905bd1f32d3ece4526e0f90906dc238f7133b24ede8446ac9740b76d}"
require_digest_pinned_image NODE_BUILD_IMAGE "$node_build_image"
require_digest_pinned_image GO_BUILD_IMAGE "$go_build_image"
require_digest_pinned_image RUNTIME_IMAGE "$runtime_image"
require_digest_pinned_image MONGO_IMAGE "$mongo_image"
build_args=(
  --load
  --file Dockerfile
  --build-arg "APP_BASE_PATH=${APP_BASE_PATH:-travel}"
  --build-arg "NODE_BUILD_IMAGE=$node_build_image"
  --build-arg "GO_BUILD_IMAGE=$go_build_image"
  --build-arg "RUNTIME_IMAGE=$runtime_image"
  --build-arg "GO_MODULE_PROXY=${GO_MODULE_PROXY:-https://proxy.golang.org,direct}"
)
docker buildx build "${build_args[@]}" --target api \
  --tag "$image_prefix-api:$image_tag" .
docker buildx build "${build_args[@]}" --target web \
  --tag "$image_prefix-web:$image_tag" .
docker compose -p my-travel --env-file "$runtime_env" -f compose.yaml stop api web >/dev/null 2>&1 || true
docker compose -p my-travel --env-file "$runtime_env" -f compose.yaml up -d --wait mongo
docker compose -p my-travel --env-file "$runtime_env" -f compose.yaml run --rm mongo-init
docker compose -p my-travel --env-file "$runtime_env" -f compose.yaml run --rm snapshot-init
docker compose -p my-travel --env-file "$runtime_env" -f compose.yaml up -d --no-build
echo "Web/API 只绑定 127.0.0.1，MongoDB 只在内部数据网络。"
echo "本地登录与健康检查可直接使用回环 HTTP。"
