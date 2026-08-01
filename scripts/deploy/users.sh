#!/usr/bin/env bash
set -euo pipefail

# 从权限受限的本机成员库初始化或追加手机号登录成员。
# 手机号和密码只从文件读取并通过请求正文发送，不进入参数或日志。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
load_project_config "$project_dir"

users_file="${USER_LIBRARY_FILE:-$project_dir/config/users.json}"
api_base="${API_BASE_URL:-}"
curl_bin="${TRAVEL_API_CURL_BIN:-curl}"
provision_all=false

die() {
  echo "$*" >&2
  exit 2
}

json_string() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '"%s"' "$value"
}

file_mode() {
  if stat -f '%Lp' "$1" >/dev/null 2>&1; then
    stat -f '%Lp' "$1"
  else
    stat -c '%a' "$1"
  fi
}

post_member() {
  local path="$1"
  local token="$2"
  local display_name="$3"
  local mobile="$4"
  local password="$5"
  local body
  body="$(
    printf '{"token":%s,"displayName":%s,"mobile":%s,"password":%s}' \
      "$(json_string "$token")" \
      "$(json_string "$display_name")" \
      "$(json_string "$mobile")" \
      "$(json_string "$password")"
  )"
  local -a args=(
    --silent --show-error --fail-with-body
    --request POST
    --url "${api_base%/}$path"
    --header "Accept: application/json"
    --header "Content-Type: application/json"
    --data-binary @-
  )
  if [[ "${API_INSECURE_TLS:-false}" == "true" ]]; then
    args+=(--insecure)
  fi
  printf '%s' "$body" | "$curl_bin" "${args[@]}" >/dev/null
}

if [[ "${1:-}" == "--provision-all" ]]; then
  provision_all=true
  shift
fi
(($# == 0)) || die "用法：./scripts/deploy/users.sh [--provision-all]"

command -v jq >/dev/null 2>&1 || die "缺少 jq，无法读取成员库。"
command -v "$curl_bin" >/dev/null 2>&1 || die "缺少 curl。"
[[ -f "$users_file" ]] ||
  die "缺少成员库：请复制 config/users.json.example 为 config/users.json。"
mode="$(file_mode "$users_file")"
[[ "$mode" == "600" || "$mode" == "400" ]] ||
  die "成员库权限必须是 600 或 400，当前为 $mode。"
[[ -n "$api_base" ]] || die "请设置 API_BASE_URL。"
[[ -n "${ADMIN_PROVISION_TOKEN:-}" ]] ||
  die "请在 config/project.env 设置 ADMIN_PROVISION_TOKEN。"
if [[ "$provision_all" != "true" ]]; then
  [[ -n "${BOOTSTRAP_TOKEN:-}" ]] ||
    die "请在 config/project.env 设置 BOOTSTRAP_TOKEN。"
fi

jq -e '
  . as $config
  | ($config.users | type == "array" and length > 0)
  and all($config.users[];
    (.mobile | type == "string" and test("^[0-9]{11}$"))
    and (.memberName | type == "string" and length > 0)
    and (.admin | type == "boolean")
    and (.password | type == "string" and length >= 6)
  )
  and (($config.users | map(.mobile) | unique | length) == ($config.users | length))
  and ([$config.users[] | select(.admin)] | length == 1)
' "$users_file" >/dev/null ||
  die "成员库格式错误；手机号必须唯一且为 11 位数字，每项必须有姓名、管理员标记和至少 6 位密码，且只能有一个管理员。"

count="$(jq '.users | length' "$users_file")"
admin_mobile=""
if [[ "$provision_all" != "true" ]]; then
  admin_name="$(jq -r '.users[] | select(.admin) | .memberName' "$users_file")"
  admin_mobile="$(jq -r '.users[] | select(.admin) | .mobile' "$users_file")"
  admin_password="$(jq -r '.users[] | select(.admin) | .password' "$users_file")"
  post_member "/auth/bootstrap" "$BOOTSTRAP_TOKEN" \
    "$admin_name" "$admin_mobile" "$admin_password"
  printf '已初始化管理员成员：%s\n' "$admin_name"
  admin_password=""
fi

for ((index = 0; index < count; index++)); do
  member_name="$(jq -r ".users[$index].memberName" "$users_file")"
  mobile="$(jq -r ".users[$index].mobile" "$users_file")"
  password="$(jq -r ".users[$index].password" "$users_file")"
  if [[ "$provision_all" != "true" && "$mobile" == "$admin_mobile" ]]; then
    password=""
    continue
  fi
  post_member "/auth/users" "$ADMIN_PROVISION_TOKEN" \
    "$member_name" "$mobile" "$password"
  printf '已新增可登录成员：%s\n' "$member_name"
  password=""
done
