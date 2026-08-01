#!/usr/bin/env bash
set -euo pipefail

# 静态检查 Nginx 模板的路径语义与凭据边界；真实证书存在性由服务器 nginx -t 校验。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config="$project_dir/deploy/nginx.conf.example"
location_config="$project_dir/deploy/nginx-location.conf.example"

[[ -f "$config" ]] || {
  echo "缺少 Nginx 配置模板：deploy/nginx.conf.example" >&2
  exit 1
}
[[ -f "$location_config" ]] || {
  echo "缺少共享网关模板：deploy/nginx-location.conf.example" >&2
  exit 1
}

required_lines=(
  'listen 80 default_server;'
  'return 308 https://__SERVER_IP__$request_uri;'
  'listen 443 ssl http2 default_server;'
  'server_name __SERVER_IP__;'
  'ssl_certificate __IP_CERTIFICATE_FULLCHAIN__;'
  'ssl_certificate_key __IP_CERTIFICATE_PRIVATE_KEY__;'
  'location = /__APP_BASE_PATH__/healthz {'
  'proxy_pass http://127.0.0.1:__APP_PORT__/healthz;'
  'location = /__APP_BASE_PATH__/api/healthz {'
  'proxy_pass http://127.0.0.1:__API_PORT__/healthz;'
  'location /__APP_BASE_PATH__/api/ {'
  'proxy_pass http://127.0.0.1:__API_PORT__/;'
  'location /_AMapService/ {'
  'proxy_pass http://127.0.0.1:__API_PORT__;'
  'location /__APP_BASE_PATH__/ {'
  'proxy_pass http://127.0.0.1:__APP_PORT__;'
  'proxy_set_header Upgrade $http_upgrade;'
  'proxy_set_header Connection $connection_upgrade;'
  'proxy_set_header X-Forwarded-Proto https;'
)

for line in "${required_lines[@]}"; do
  grep -Fq "$line" "$config" || {
    echo "Nginx 模板缺少必要配置：$line" >&2
    exit 1
  }
done

for line in \
  'location = /__APP_BASE_PATH__/healthz {' \
  'proxy_pass http://127.0.0.1:__APP_PORT__/healthz;' \
  'location /__APP_BASE_PATH__/api/ {' \
  'proxy_pass http://127.0.0.1:__API_PORT__/;' \
  'location /_AMapService/ {' \
  'proxy_pass http://127.0.0.1:__API_PORT__;' \
  'location /__APP_BASE_PATH__/ {' \
  'proxy_set_header X-Forwarded-Prefix /__APP_BASE_PATH__;'; do
  grep -Fq "$line" "$location_config" || {
    echo "共享网关模板缺少必要配置：$line" >&2
    exit 1
  }
done

if grep -Fq '$connection_upgrade' "$location_config"; then
  echo "共享网关模板不能依赖宿主机未定义的 connection_upgrade 变量。" >&2
  exit 1
fi

if grep -Eq 'BEGIN ([A-Z ]+)?PRIVATE KEY|47[.]98[.]138[.]149' "$config" "$location_config"; then
  echo "Nginx 模板不得包含私钥正文或真实服务器 IP。" >&2
  exit 1
fi

echo "Nginx 模板静态检查通过"
