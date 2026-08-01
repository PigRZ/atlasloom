#!/usr/bin/env bash
set -euo pipefail

# 静态检查两层部署、登录成员库与数据库默认保留边界。

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

required_files=(
  config/project.env.example
  config/users.json.example
  scripts/deploy/users.sh
  scripts/deploy/backup.sh
  scripts/deploy/environment.sh
  scripts/deploy/project.sh
  scripts/deploy/database.sh
  scripts/deploy/project-remote.sh
)
for file in "${required_files[@]}"; do
  [[ -f "$project_dir/$file" ]] || {
    echo "缺少服务器工作流文件：$file" >&2
    exit 1
  }
done

awk '
  /^[A-Z][A-Z0-9_]*=/ {
    if (previous_line !~ /^# 获取\/填写：/ ||
        previous_previous_line !~ /^# 用途/) {
      print "配置示例缺少逐项用途或获取说明：" $0 > "/dev/stderr"
      failed = 1
    }
  }
  {
    previous_previous_line = previous_line
    previous_line = $0
  }
  END {
    exit failed
  }
' "$project_dir/config/project.env.example"

grep -Fq '/config/users.json' "$project_dir/.gitignore" || {
  echo "用户库必须被 Git 忽略。" >&2
  exit 1
}
grep -Fq 'mongodump --quiet --archive --gzip' "$project_dir/scripts/deploy/backup.sh" || {
  echo "服务器重建前必须下载完整数据库恢复包。" >&2
  exit 1
}
grep -Fq 'RESET-$DEPLOY_PROJECT' "$project_dir/scripts/deploy/environment.sh" || {
  echo "环境重建缺少项目级二次确认。" >&2
  exit 1
}
if grep -Fq -- '--replace-database' "$project_dir/scripts/deploy/project.sh"; then
  echo "项目部署脚本不得提供数据库覆盖入口。" >&2
  exit 1
fi
grep -Fq 'RESTORE-$DEPLOY_PROJECT' "$project_dir/scripts/deploy/database.sh" || {
  echo "数据库覆盖缺少二次确认。" >&2
  exit 1
}
grep -Fq 'dropDatabase()' "$project_dir/scripts/deploy/database.sh" &&
  grep -Fq '数据库切换失败，已恢复发布前数据库、快照和 release。' \
    "$project_dir/scripts/deploy/database.sh" || {
  echo "数据库覆盖必须精确替换目标库并支持失败回滚。" >&2
  exit 1
}
grep -Fq 'target_sha="$(git -C "$project_dir" rev-parse HEAD)"' \
  "$project_dir/scripts/deploy/database.sh" || {
  echo "数据库覆盖必须与本地确切提交对应的候选 release 一起切换。" >&2
  exit 1
}
grep -Fq 'up -d --no-build api web' "$project_dir/scripts/deploy/database.sh" || {
  echo "数据库恢复后必须补建一次性依赖并启动应用。" >&2
  exit 1
}
grep -Fq '未发现现有数据库容器，正在建立空数据库环境。' \
  "$project_dir/scripts/deploy/project-remote.sh" || {
  echo "项目部署尚不支持新服务器首次启动。" >&2
  exit 1
}
grep -Fq '数据库容器不符合当前 Compose 数据网络定义，发布已停止。' \
  "$project_dir/scripts/deploy/project-remote.sh" || {
  echo "项目部署必须拒绝非当前 Compose 数据网络。" >&2
  exit 1
}
if grep -Fq 'docker network connect' "$project_dir/scripts/deploy/project-remote.sh"; then
  echo "项目部署不得自动改造运行中的数据库网络。" >&2
  exit 1
fi
grep -Fq '本地 Docker Compose' "$project_dir/scripts/deploy/project.sh" || {
  echo "本地部署必须明确使用 Docker Compose。" >&2
  exit 1
}
grep -Fq 'docker buildx build' "$project_dir/scripts/start.sh" &&
  grep -Fq 'up -d --no-build' "$project_dir/scripts/start.sh" || {
  echo "本地部署必须用 Buildx 构建并由 Compose 启动。" >&2
  exit 1
}
grep -Fq 'docker save "$api_image" "$web_image"' \
  "$project_dir/scripts/deploy/project-remote.sh" &&
  grep -Fq 'docker load' "$project_dir/scripts/deploy/project-remote.sh" &&
  grep -Fq 'actual_api_image_id' "$project_dir/scripts/deploy/project-remote.sh" || {
  echo "远程发布必须传输并校验本地构建的确切镜像制品。" >&2
  exit 1
}
if grep -Eq '"?\\$\\{?compose[^[:space:]]*\\}?"?[[:space:]]+build|docker compose.* build' \
  "$project_dir/scripts/deploy/project-remote.sh"; then
  echo "远程发布不得在服务器重新构建应用镜像。" >&2
  exit 1
fi
grep -Fq 'require_digest_pinned_image NODE_BUILD_IMAGE' "$project_dir/scripts/start.sh" &&
  grep -Fq 'require_digest_pinned_image MONGO_IMAGE' "$project_dir/scripts/start.sh" &&
  grep -Fq '@sha256:' "$project_dir/Dockerfile" &&
  grep -Fq '@sha256:' "$project_dir/compose.yaml" || {
  echo "构建与运行基础镜像必须固定到不可变摘要。" >&2
  exit 1
}
grep -Fq -- '--provision-all' "$project_dir/scripts/deploy/users.sh" || {
  echo "登录成员库脚本必须支持已有数据库批量追加成员。" >&2
  exit 1
}
grep -Fq '.mobile | type == "string" and test("^[0-9]{11}$")' \
  "$project_dir/scripts/deploy/users.sh" || {
  echo "登录成员库必须校验 11 位手机号。" >&2
  exit 1
}

test_dir="$(mktemp -d "${TMPDIR:-/tmp}/my-travel-users-test.XXXXXX")"
trap 'rm -rf "$test_dir"' EXIT
users_fixture="$test_dir/users.json"
mock_curl="$test_dir/mock-curl"
request_log="$test_dir/requests.log"
cat > "$users_fixture" <<'JSON'
{
  "users": [
    {
      "mobile": "10000000000",
      "password": "admin-pass",
      "memberName": "测试管理员",
      "admin": true
    },
    {
      "mobile": "10000000001",
      "password": "member-pass-1",
      "memberName": "测试成员甲",
      "admin": false
    },
    {
      "mobile": "10000000002",
      "password": "member-pass-2",
      "memberName": "测试成员乙",
      "admin": false
    }
  ]
}
JSON
cat > "$mock_curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
request_body="$(cat)"
jq -e '
  (.token | type == "string" and length > 0)
  and (.displayName | type == "string" and length > 0)
  and (.mobile | type == "string" and test("^[0-9]{11}$"))
  and (.password | type == "string" and length >= 6)
' <<<"$request_body" >/dev/null
printf '%s\n' "$*" >> "$USER_PROVISION_TEST_LOG"
MOCK
chmod 600 "$users_fixture"
chmod 700 "$mock_curl"
: > "$request_log"
USER_LIBRARY_FILE="$users_fixture" \
  API_BASE_URL="https://travel.example.com/travel/api" \
  TRAVEL_API_CURL_BIN="$mock_curl" \
  USER_PROVISION_TEST_LOG="$request_log" \
  BOOTSTRAP_TOKEN="bootstrap-test-token" \
  ADMIN_PROVISION_TOKEN="admin-test-token" \
  "$project_dir/scripts/deploy/users.sh" >/dev/null
[[ "$(wc -l < "$request_log" | tr -d ' ')" == "3" ]] || {
  echo "合法的三人登录成员库应产生一次初始化和两次新增请求。" >&2
  exit 1
}
[[ "$(grep -Fc '/auth/bootstrap' "$request_log")" == "1" ]] || {
  echo "登录成员库应且仅应初始化一个管理员。" >&2
  exit 1
}
[[ "$(grep -Fc '/auth/users' "$request_log")" == "2" ]] || {
  echo "登录成员库应新增其余两个成员。" >&2
  exit 1
}
echo "服务器两层部署工作流检查通过"
