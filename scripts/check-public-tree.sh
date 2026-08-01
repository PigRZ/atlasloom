#!/usr/bin/env bash
set -euo pipefail

# 公开树门禁只报告敏感值类别，不把本机真实值回显到日志。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"

command -v git >/dev/null 2>&1 || {
  echo "公开树检查需要 Git。" >&2
  exit 2
}
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "当前目录不是 Git 工作树，无法确认公开文件集合。" >&2
  exit 2
}

failed=false

if git ls-files | grep -Eq \
  '^(config/project\.env|config/users\.json)$|^(\.local/|backups/|snapshots/|seed_data/|\.deploy/)|(^|/)(id_rsa|id_ed25519)(\.|$)|\.(pem|key|p12|pfx|htpasswd)$|login-qr\.png$'; then
  echo "公开树包含本应忽略的凭据、快照、备份或运行文件。" >&2
  failed=true
fi

if git grep --untracked --exclude-standard -I -q -E \
  -e '-----BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----|/Users/[A-Za-z0-9._-]+/|/home/[A-Za-z0-9._-]+/' \
  -- .; then
  echo "公开树包含私钥正文或用户级绝对路径。" >&2
  failed=true
fi

while IFS= read -r line; do
  scrubbed="${line//13800138000/}"
  scrubbed="${scrubbed//13900139000/}"
  scrubbed="${scrubbed//13700137000/}"
  if [[ "$scrubbed" =~ (^|[^0-9])1[3-9][0-9]{9}([^0-9]|$) ]]; then
    echo "公开树包含未列入测试白名单的中国大陆手机号格式。" >&2
    failed=true
    break
  fi
done < <(
  git grep --untracked --exclude-standard -I -n -E \
    -e '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)' -- . || true
)

while IFS= read -r line; do
  scrubbed="${line//@example.com/}"
  scrubbed="${scrubbed//@example.test/}"
  scrubbed="${scrubbed//@users.noreply.github.com/}"
  if [[ "$scrubbed" =~ [A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,} ]]; then
    echo "公开树包含非示例、非 GitHub noreply 的电子邮箱地址。" >&2
    failed=true
    break
  fi
done < <(
  git grep --untracked --exclude-standard -I -n -E \
    -e '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' -- . || true
)

config_value() {
  local target="$1"
  local file="$project_dir/config/project.env"
  local raw line key value
  [[ -f "$file" ]] || return 0
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    line="${raw#"${raw%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "${line:0:1}" == "#" || "$line" != *=* ]] && continue
    key="${line%%=*}"
    key="${key#"${key%%[![:space:]]*}"}"
    key="${key%"${key##*[![:space:]]}"}"
    [[ "$key" == "$target" ]] || continue
    value="${line#*=}"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    if [[ ${#value} -ge 2 ]] &&
      { [[ "${value:0:1}" == '"' && "${value: -1}" == '"' ]] ||
        [[ "${value:0:1}" == "'" && "${value: -1}" == "'" ]]; }; then
      value="${value:1:${#value}-2}"
    fi
    printf '%s' "$value"
    return 0
  done < "$file"
}

scan_local_value() {
  local key="$1"
  local minimum_length="$2"
  local value="${!key-}"
  if [[ -z "$value" ]]; then
    value="$(config_value "$key")"
  fi
  [[ -n "$value" && ${#value} -ge minimum_length ]] || return 0
  if git grep --untracked --exclude-standard -I -F -q -e "$value" -- .; then
    echo "公开树包含本机配置项 $key 的真实值。" >&2
    failed=true
  fi
}

scan_local_value AMAP_WEBSERVICE_KEY 8
scan_local_value AMAP_JSAPI_KEY 8
scan_local_value AMAP_JSAPI_SECURITY_CODE 8
scan_local_value ADMIN_PROVISION_TOKEN 8
scan_local_value BOOTSTRAP_TOKEN 8
scan_local_value API_DISPLAY_NAME 3
scan_local_value API_MOBILE 11
scan_local_value API_BASE_URL 8
scan_local_value DEPLOY_HOST 4
scan_local_value SSH_KEY 8
scan_local_value USER_LIBRARY_FILE 8
scan_local_value TLS_CERTIFICATE_FULLCHAIN 8
scan_local_value TLS_CERTIFICATE_PRIVATE_KEY 8

if [[ "$failed" == "true" ]]; then
  exit 1
fi

echo "公开树隐私检查通过（完整 Git 历史仍需在发布前单独扫描）"
