#!/usr/bin/env bash
set -euo pipefail

# 在一次性副本集上运行真实 MongoDB validator、索引和事务集成测试。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
mongo_image="${MONGO_TEST_IMAGE:-mongo:8.0-noble@sha256:277f9152905bd1f32d3ece4526e0f90906dc238f7133b24ede8446ac9740b76d}"
container_name="my-travel-mongo-test-$$"
coverage_file="$(mktemp -t my-travel-go-coverage.XXXXXX)"
current_snapshot_dir="${CURRENT_SNAPSHOT_DIR:-}"

if [[ -n "$current_snapshot_dir" ]]; then
  [[ -d "$current_snapshot_dir" ]] || {
    echo "CURRENT_SNAPSHOT_DIR 不是可读目录：$current_snapshot_dir" >&2
    exit 2
  }
  current_snapshot_dir="$(cd "$current_snapshot_dir" && pwd -P)"
fi

command -v docker >/dev/null 2>&1 || {
  echo "MongoDB 集成测试需要 Docker。" >&2
  exit 2
}
docker info >/dev/null 2>&1 || {
  echo "MongoDB 集成测试需要可用的 Docker daemon。" >&2
  exit 2
}
require_digest_pinned_image MONGO_TEST_IMAGE "$mongo_image"

cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  rm -f "$coverage_file"
}
trap cleanup EXIT

docker run --rm --detach \
  --name "$container_name" \
  --publish 127.0.0.1::27017 \
  --tmpfs /data/db \
  "$mongo_image" \
  --replSet rs0 --bind_ip_all >/dev/null

ready=false
for _ in $(seq 1 40); do
  if docker exec "$container_name" \
    mongosh --quiet --eval 'db.adminCommand({ping: 1}).ok' >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
[[ "$ready" == "true" ]] || {
  echo "一次性 MongoDB 未能启动。" >&2
  exit 1
}

docker exec "$container_name" mongosh --quiet --eval \
  'rs.initiate({_id:"rs0",members:[{_id:0,host:"localhost:27017"}]})' >/dev/null

primary=false
for _ in $(seq 1 40); do
  if [[ "$(docker exec "$container_name" mongosh --quiet --eval \
    'db.hello().isWritablePrimary' 2>/dev/null)" == "true" ]]; then
    primary=true
    break
  fi
  sleep 1
done
[[ "$primary" == "true" ]] || {
  echo "一次性 MongoDB 副本集未选出主节点。" >&2
  exit 1
}

host_port="$(docker port "$container_name" 27017/tcp | sed -n 's/.*://p' | head -1)"
[[ "$host_port" =~ ^[0-9]+$ ]] || {
  echo "无法解析一次性 MongoDB 端口。" >&2
  exit 1
}

(
  cd "$project_dir/server"
  MONGODB_TEST_URI="mongodb://127.0.0.1:$host_port/?replicaSet=rs0&directConnection=true" \
    CURRENT_SNAPSHOT_DIR="$current_snapshot_dir" \
    go test ./... -coverpkg=./... -coverprofile="$coverage_file" -count=1
)

coverage_percent="$(
  cd "$project_dir/server"
  go tool cover -func="$coverage_file" | awk '/^total:/ {gsub("%", "", $3); print $3}'
)"
minimum_coverage="${GO_COVERAGE_MIN:-35}"
awk -v actual="$coverage_percent" -v minimum="$minimum_coverage" \
  'BEGIN { exit !(actual + 0 >= minimum + 0) }' || {
  echo "Go 总覆盖率 ${coverage_percent}% 低于最低要求 ${minimum_coverage}%。" >&2
  exit 1
}

echo "Go 总覆盖率：${coverage_percent}%（最低 ${minimum_coverage}%）"
echo "真实 MongoDB 集成测试通过"
