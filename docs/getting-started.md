# 安装与部署

本手册覆盖本机 Docker 启动、首次管理员、可选高德能力和 Linux HTTPS 部署。数据库
默认只在 Compose 内部网络，Web/API 只映射到宿主机回环地址。

## 支持范围

| 场景 | 支持状态 | 要求 |
|---|---|---|
| macOS 本地运行 | 支持 | Docker Desktop、Git、OpenSSL |
| Linux 本地运行 | 支持 | Docker Engine、Compose、Buildx、Git、OpenSSL |
| Windows | 建议 WSL2 | 在 WSL2 内使用 Git 与 Docker Desktop 集成 |
| 原生 Windows Shell | 未验证 | Bash 脚本和权限语义不保证兼容 |
| Linux 远程 HTTPS | 支持 | SSH、Docker、Nginx、systemd、有效证书 |

普通启动全部使用固定摘要的容器构建镜像，不要求宿主机 Go/Node。运行
`./scripts/test.sh` 需要 Go 1.26、Node.js 24+ 和 Docker；使用 AI 计划编译也需要
Go 1.26。

## 本机安装

### 1. 获取代码并创建受限配置

```bash
git clone <your-repository-url> atlasloom
cd atlasloom
cp config/project.env.example config/project.env
chmod 600 config/project.env
```

默认配置可直接用于本地：

```text
APP_BASE_PATH=travel
APP_PORT=8501
API_PORT=8502
API_BASE_URL=http://127.0.0.1:8502
COOKIE_SECURE=false
```

不要提交 `config/project.env`。它已被 `.gitignore` 排除，且公开树门禁会阻止它意外
进入跟踪文件。

### 2. 可选：配置高德

不配置高德仍可使用日程、成员、费用和资料；地图搜索、路线、天气会显示不可用状态。
需要时在 `config/project.env` 填写：

```text
AMAP_WEBSERVICE_KEY=
AMAP_JSAPI_KEY=
AMAP_JSAPI_SECURITY_CODE=
```

- Web Service Key 供 Go API 和项目级高德 Skill 查询地点、路线、天气。
- JSAPI Key 与 security code 必须成对配置，供浏览器地图使用。
- Key 只放受限配置；不要写进 URL 参数示例、Skill 配置、Issue 或日志。

### 3. 启动

```bash
./scripts/start.sh
```

脚本会：

1. 检查 Docker Compose 与 Buildx。
2. 在 `.local/app/runtime.env` 生成只用于本机首次初始化的随机 bootstrap token。
3. 用固定基础镜像摘要构建 API 与 Web。
4. 启动 MongoDB 单节点副本集，安装当前严格 validator、索引和契约指纹。
5. 启动只绑定 `127.0.0.1` 的 API 与 Web。

检查：

```bash
curl --fail http://127.0.0.1:8502/healthz
curl --fail http://127.0.0.1:8501/healthz
docker compose -p my-travel --env-file .local/app/runtime.env ps
```

### 4. 创建首个管理员

空数据库只允许 bootstrap 一次：

```bash
./scripts/travel-api auth bootstrap
```

本地回环地址时，脚本自动读取 `.local/app/runtime.env` 的 token；终端只询问：

1. 成员显示名称
2. 11 位登录手机号
3. 新密码
4. 再次输入密码

Token 和密码不会进入命令参数。Bootstrap 成功后：

```bash
./scripts/travel-api auth login
```

浏览器打开 <http://127.0.0.1:8501/travel/>。如果数据库已经有管理员，再运行 bootstrap
会被拒绝，这是预期行为。

### 5. 停止、重启和保留数据

停止容器但保留 `.local/docker/mongo` 与快照：

```bash
docker compose -p my-travel --env-file .local/app/runtime.env down
```

重新运行 `./scripts/start.sh` 会重建当前代码并复用已有数据库。不要运行带 `-v` 的
删除命令，也不要手工删除 `.local/docker/mongo`，除非你已经有可验证备份并明确要重置。

升级代码前先在“所有旅行”导出每场旅行的备份。开发者发布前还必须登录管理员账号并
运行 `./scripts/travel-data pull-all`：自己作为 owner 的旅行会生成可编辑工作区；
管理员不是 owner 的旅行只保存完整备份和 `readonly.json`，不能执行 diff、sync 或
restore；非管理员 editor/viewer 会在替换任何本地文件前终止整批拉取。详细边界见
[真实数据工作流](data-workflow.md#修改攻略)。

## 新增登录成员

在服务器受限配置中设置随机 `ADMIN_PROVISION_TOKEN`，本机配置使用相同值，再运行：

```bash
./scripts/travel-api auth create-user
```

新增登录成员不会自动获得任何旅行权限。管理员需要在具体旅行的“成员”页面选择该基础
成员。只需同行、无需登录的成员可直接在“基础资料 → 成员”创建。

批量初始化方式和权限细节见 [高级运维](operations.md#新增用户)。

## 远程 Linux HTTPS 部署

远程脚本面向 Debian/Ubuntu、使用 apt 的系统，以及提供 yum/dnf 的兼容发行版；要求
Bash、OpenSSH、Docker/Buildx/Compose、Nginx 与 systemd。它不是通用 Kubernetes 或
Windows 部署器。

### 1. 本机填写发布参数

在被忽略的 `config/project.env` 填写：

```text
COOKIE_SECURE=true
API_BASE_URL=https://travel.example.com/travel/api
DEPLOY_HOST=travel.example.com
DEPLOY_USER=root
DEPLOY_DIR=/opt/my-travel
DEPLOY_PROJECT=my-travel
DEPLOY_PLATFORM=linux/amd64
NGINX_MODE=standalone
TLS_CERTIFICATE_FULLCHAIN=/服务器上的证书完整链绝对路径
TLS_CERTIFICATE_PRIVATE_KEY=/服务器上的证书私钥绝对路径
```

已有共享 HTTPS 网关时使用 `NGINX_MODE=shared`。SSH 私钥只填本机绝对路径或交给
ssh-agent；私钥本身不得复制到仓库。

### 2. 先构建环境

```bash
./scripts/deploy/environment.sh
```

该脚本安装/检查依赖、创建受限目录和 Nginx 入口，不发布业务代码，也不覆盖 MongoDB。

### 3. 本地优先发布

先确保 `travel-api` 当前登录的是管理员账号，再执行：

```bash
./scripts/travel-api auth login
./scripts/travel-data pull-all
./scripts/test.sh
BOOTSTRAP_TOKEN=static-check docker compose config
git status --short
./scripts/deploy/project.sh remote
```

发布前的全量保护依赖管理员已有的备份导出权限，不会授予其快照查看、恢复或资源写入
权限。`pull-all` 中的只读管理员备份也不得提交 Git 或作为可编辑攻略同步。

远程只接收本机构建并校验的确切提交和镜像；服务器不会重新构建，也不得在 release 内
直接调试或改代码。代码发布不导入、覆盖或重置业务数据。

### 4. 数据变化单独处理

攻略变化使用 HTTPS API 的快照 diff/sync 或旅行备份导入。不要 SSH 进入 MongoDB。
完整顺序见 [真实数据工作流](data-workflow.md)，备份、发布锁、Nginx、回滚和数据库灾备
见 [高级运维、发布与恢复](operations.md)。

## 常见问题

### Docker 构建在含非 ASCII 路径的目录失败

`start.sh` 已显式调用 Buildx，避免部分 Docker Desktop Bake 会话问题。仍失败时把仓库
放到简短 ASCII 路径再重试；移动工作区前先验证并保留 `.local` 数据副本。

### 健康检查报告数据库契约不一致

API 会先只读检查已有文档，再安装当前 validator。发现任何不符合当前模型的文档时启动
必须停止。在本地隔离数据库用完整备份重建符合当前契约的候选库；不要在服务器关闭
校验器或直接改文档。

### 地图或天气不可用

检查三项高德配置是否对应正确的平台类型和白名单。接口失败时系统应显示缓存时间和失败
状态，不会伪造实时数据。
