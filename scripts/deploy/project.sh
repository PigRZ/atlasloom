#!/usr/bin/env bash
set -euo pipefail

# 第二层统一入口：选择部署到本地 Docker Compose 或远程服务器，并始终保留数据库。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$project_dir/scripts/lib/project-config.sh"
load_project_config "$project_dir"

target=""
while (($# > 0)); do
  case "$1" in
    local|remote)
      [[ -z "$target" ]] || {
        echo "只能选择一个部署目标。" >&2
        exit 2
      }
      target="$1"
      shift
      ;;
    *)
      echo "用法：./scripts/deploy/project.sh [local|remote]" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$target" ]]; then
  [[ -t 0 ]] || {
    echo "非交互执行时必须指定 local 或 remote。" >&2
    exit 2
  }
  printf '选择部署目标：\n1）本地\n2）远程服务器\n'
  read -r -p "请输入 1 或 2：" choice
  case "$choice" in
    1) target=local ;;
    2) target=remote ;;
    *) echo "无效选择。" >&2; exit 2 ;;
  esac
fi

echo "数据库策略：保留现有数据。数据库覆盖请单独运行 scripts/deploy/database.sh。"

if [[ "$target" == "local" ]]; then
  echo "部署目标：本地 Docker Compose。"
  "$project_dir/scripts/start.sh"
  exit 0
fi

echo "部署目标：远程服务器 Docker Compose。"
"$project_dir/scripts/deploy/project-remote.sh"
