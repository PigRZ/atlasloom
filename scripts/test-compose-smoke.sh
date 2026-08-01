#!/usr/bin/env bash
set -euo pipefail

# 用隔离目录和临时端口验证生产容器、同源代理、安全边界与缓存策略。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
command -v docker >/dev/null 2>&1 || {
  echo "缺少 Docker，无法运行容器烟测。" >&2
  exit 1
}
docker info >/dev/null 2>&1 || {
  echo "Docker 当前不可用，无法运行容器烟测。" >&2
  exit 1
}
docker compose version >/dev/null 2>&1 || {
  echo "缺少 Docker Compose，无法运行容器烟测。" >&2
  exit 1
}
docker buildx version >/dev/null 2>&1 || {
  echo "缺少 Docker Buildx，无法构建容器烟测镜像。" >&2
  exit 1
}
command -v curl >/dev/null 2>&1 || {
  echo "缺少 curl，无法验证容器 HTTP 边界。" >&2
  exit 1
}
command -v python3 >/dev/null 2>&1 || {
  echo "缺少 Python 3，无法分配隔离测试端口。" >&2
  exit 1
}

smoke_dir="$(mktemp -d "${TMPDIR:-/tmp}/my-travel-compose-smoke.XXXXXX")"
suffix="$(basename "$smoke_dir" | tr '[:upper:]' '[:lower:]')"
compose_project="my-travel-smoke-${suffix##*.}"
deploy_project="$compose_project"
runtime_env="$smoke_dir/runtime.env"

free_port() {
  python3 - <<'PY'
import socket

with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
}

app_port="$(free_port)"
api_port="$(free_port)"
if [[ "$app_port" == "$api_port" ]]; then
  api_port="$(free_port)"
fi
mkdir -p "$smoke_dir/mongo" "$smoke_dir/snapshots"
{
  printf 'APP_BASE_PATH=travel\n'
  printf 'APP_PORT=%s\n' "$app_port"
  printf 'API_PORT=%s\n' "$api_port"
  printf 'COOKIE_SECURE=false\n'
  printf 'MONGODB_DATABASE=my_travel_smoke\n'
  printf 'BOOTSTRAP_TOKEN=compose-smoke-bootstrap-token\n'
  printf 'DEPLOY_PROJECT=%s\n' "$deploy_project"
  printf 'RELEASE_IMAGE_TAG=smoke\n'
  printf 'MONGO_DATA_DIR=%s\n' "$smoke_dir/mongo"
  printf 'SNAPSHOT_DATA_DIR=%s\n' "$smoke_dir/snapshots"
} > "$runtime_env"
chmod 600 "$runtime_env"

compose=(
  docker compose
  -p "$compose_project"
  --env-file "$runtime_env"
  -f "$project_dir/compose.yaml"
)

# 构建和清理都只使用固定 digest；Linux runner 上 Mongo 会把 bind mount 写成 root。
node_build_image="${NODE_BUILD_IMAGE:-node:24-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d}"
go_build_image="${GO_BUILD_IMAGE:-golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651}"
runtime_image="${RUNTIME_IMAGE:-alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce}"

cleanup() {
  local exit_code=$?
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  docker image rm \
    "$deploy_project-api:smoke" \
    "$deploy_project-web:smoke" \
    >/dev/null 2>&1 || true
  case "$smoke_dir" in
    "${TMPDIR:-/tmp}"/my-travel-compose-smoke.*)
      if ! docker run --rm --network none \
        -v "$smoke_dir:/cleanup" \
        "$runtime_image" \
        sh -c "chown -R $(id -u):$(id -g) /cleanup" \
        >/dev/null; then
        echo "无法恢复容器烟测临时目录的宿主机所有权：$smoke_dir" >&2
        exit_code=1
      fi
      if ! rm -rf -- "$smoke_dir"; then
        echo "无法清理容器烟测临时目录：$smoke_dir" >&2
        exit_code=1
      fi
      ;;
  esac
  return "$exit_code"
}
trap cleanup EXIT

# Compose 5 的 Bake 会话不能可靠处理含非 ASCII 字符的工作区路径。
# 与本地发布一致，先显式构建确切镜像，再让 Compose 只负责运行拓扑。
build_args=(
  --load
  --file "$project_dir/Dockerfile"
  --build-arg "APP_BASE_PATH=travel"
  --build-arg "NODE_BUILD_IMAGE=$node_build_image"
  --build-arg "GO_BUILD_IMAGE=$go_build_image"
  --build-arg "RUNTIME_IMAGE=$runtime_image"
  --build-arg "GO_MODULE_PROXY=${GO_MODULE_PROXY:-https://proxy.golang.org,direct}"
)
docker buildx build "${build_args[@]}" \
  --target api \
  --tag "$deploy_project-api:smoke" \
  "$project_dir"
docker buildx build "${build_args[@]}" \
  --target web \
  --tag "$deploy_project-web:smoke" \
  "$project_dir"
"${compose[@]}" up -d --no-build --wait --wait-timeout 240

web_url="http://127.0.0.1:$app_port"
api_url="http://127.0.0.1:$api_port"
curl -fsS "$web_url/healthz" >/dev/null
curl -fsS "$api_url/healthz" >/dev/null
curl -fsS "$web_url/travel/" >/dev/null
curl -fsS "$web_url/travel/api/healthz" >/dev/null

protected_status="$(
  curl -sS -o /dev/null -w '%{http_code}' "$web_url/travel/api/trips"
)"
[[ "$protected_status" == "401" ]] || {
  echo "未登录旅行接口应返回 401，实际为 $protected_status。" >&2
  exit 1
}
unknown_route_status="$(
  curl -sS -o /dev/null -w '%{http_code}' "$web_url/travel/api/unsupported-route"
)"
[[ "$unknown_route_status" == "404" ]] || {
  echo "未知接口应返回 404，实际为 $unknown_route_status。" >&2
  exit 1
}

web_headers="$(curl -fsSI "$web_url/travel/" | tr -d '\r')"
grep -Fiq 'Cache-Control: no-cache' <<<"$web_headers" || {
  echo "SPA 入口缺少 no-cache 策略。" >&2
  exit 1
}
grep -Fiq 'X-Frame-Options: DENY' <<<"$web_headers" || {
  echo "Web 响应缺少防嵌入响应头。" >&2
  exit 1
}
api_headers="$(curl -fsSI "$web_url/travel/api/healthz" | tr -d '\r')"
grep -Fiq 'Cache-Control: no-store' <<<"$api_headers" || {
  echo "API 响应缺少 no-store 策略。" >&2
  exit 1
}

index_html="$(curl -fsS "$web_url/travel/")"
asset_path="$(
  grep -oE '/travel/assets/[^" ]+[.]js' <<<"$index_html" | head -1
)"
[[ -n "$asset_path" ]] || {
  echo "生产入口未引用指纹化 JavaScript 资源。" >&2
  exit 1
}
asset_headers="$(curl -fsSI "$web_url$asset_path" | tr -d '\r')"
grep -Fiq 'Cache-Control: public, max-age=31536000, immutable' \
  <<<"$asset_headers" || {
  echo "指纹化静态资源缺少长期不可变缓存策略。" >&2
  exit 1
}

mongo_id="$("${compose[@]}" ps -q mongo)"
[[ -n "$mongo_id" ]] || {
  echo "MongoDB 容器未运行。" >&2
  exit 1
}
if docker port "$mongo_id" 27017/tcp 2>/dev/null | grep -q .; then
  echo "MongoDB 不得发布宿主机端口。" >&2
  exit 1
fi
for service in mongo api web; do
  container_id="$("${compose[@]}" ps -q "$service")"
  health="$(docker inspect --format '{{.State.Health.Status}}' "$container_id")"
  [[ "$health" == "healthy" ]] || {
    echo "$service 容器未达到健康状态：$health" >&2
    exit 1
  }
done
for service in mongo-init snapshot-init; do
  container_id="$("${compose[@]}" ps -aq "$service")"
  exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container_id")"
  [[ "$exit_code" == "0" ]] || {
    echo "$service 一次性容器执行失败：$exit_code" >&2
    exit 1
  }
done

echo "容器启动、回环端口、鉴权边界与缓存策略烟测通过"
