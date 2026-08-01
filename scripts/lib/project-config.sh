#!/usr/bin/env bash

# 公共配置库只解析 KEY=value 与安全更新环境文件，被 source 时不产生副作用。

trim_config_value() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  if [[ ${#value} -ge 2 ]]; then
    if [[ "${value:0:1}" == '"' && "${value: -1}" == '"' ]] ||
       [[ "${value:0:1}" == "'" && "${value: -1}" == "'" ]]; then
      value="${value:1:${#value}-2}"
    fi
  fi
  printf '%s' "$value"
}

load_env_file_defaults() {
  local config_file="$1"
  local raw_line line key value line_number=0
  [[ -f "$config_file" ]] || return 0

  while IFS= read -r raw_line || [[ -n "$raw_line" ]]; do
    line_number=$((line_number + 1))
    line="$(trim_config_value "$raw_line")"
    [[ -z "$line" || "${line:0:1}" == "#" ]] && continue
    if [[ "$line" != *=* ]]; then
      echo "配置文件格式错误：$config_file 第 $line_number 行缺少 =。" >&2
      return 2
    fi
    key="$(trim_config_value "${line%%=*}")"
    value="$(trim_config_value "${line#*=}")"
    if [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
      echo "配置文件格式错误：$config_file 第 $line_number 行变量名无效。" >&2
      return 2
    fi
    if [[ -z "${!key-}" ]]; then
      printf -v "$key" '%s' "$value"
      export "$key"
    fi
  done < "$config_file"
}

load_project_config() {
  local project_dir="$1"
  local config_file="${TRAVEL_CONFIG_FILE:-$project_dir/config/project.env}"
  load_env_file_defaults "$config_file"
}

set_env_file_value() {
  local env_file="$1"
  local key="$2"
  local value="$3"
  local temp_file line found=0
  temp_file="$(mktemp "${env_file}.XXXXXX")"
  chmod 600 "$temp_file"
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == "$key="* ]]; then
      printf '%s=%s\n' "$key" "$value" >> "$temp_file"
      found=1
    else
      printf '%s\n' "$line" >> "$temp_file"
    fi
  done < "$env_file"
  if ((found == 0)); then
    printf '%s=%s\n' "$key" "$value" >> "$temp_file"
  fi
  mv "$temp_file" "$env_file"
}

require_digest_pinned_image() {
  local key="$1"
  local value="$2"
  if [[ ! "$value" =~ @sha256:[0-9a-f]{64}$ ]]; then
    echo "$key 必须固定到不可变镜像摘要（格式：镜像@sha256:64位摘要），当前值：$value" >&2
    return 2
  fi
}
