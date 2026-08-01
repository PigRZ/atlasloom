# 启动、发布与恢复

首次安装、bootstrap 和最小远程部署入口见 [安装与部署](getting-started.md)。本页保留
高级发布、Nginx、灾备与恢复细节。

## 两层部署模型

服务器部署明确分成两层：

1. `scripts/deploy/environment.sh`：安装或检查 Docker、Compose、Nginx、curl，建立共享目录，
   从本机 `config/project.env` 写入服务器权限为 `600` 的运行配置，并安装网关配置。
2. `scripts/deploy/project.sh`：选择部署到 `local` 或 `remote`，发布本地确切提交。

项目部署不包含数据库覆盖入口，因此始终保留数据库。覆盖数据库只能单独执行
`scripts/deploy/database.sh <本地恢复包>` 并完成二次确认。

## 本地环境

准备不入库配置：

```bash
cp config/project.env.example config/project.env
chmod 600 config/project.env
./scripts/start.sh
```

默认端口：

- Web：`127.0.0.1:8501`
- API：`127.0.0.1:8502`
- MongoDB：仅 Compose 数据网络，不映射宿主机

本地及容器直连 Web 时，Web 进程通过 `API_UPSTREAM_URL=http://api:8502` 将
`/{APP_BASE_PATH}/api/` 去除前缀后转发到 API。生产 Nginx 会在请求到达 Web 前直接
分流该路径，所以这层代理只承担无外层网关时的同源兜底。高德 JSAPI 安全模式固定使用
域名后的一级路径 `/_AMapService/`；本地 Web 原样转发该路径，Go API 校验登录会话后
才向高德请求注入服务端安全密钥。

常用检查：

```bash
curl --fail http://127.0.0.1:8501/healthz
curl --fail http://127.0.0.1:8502/healthz
curl -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8501/_AMapService/v3/direction/driving
docker compose -p my-travel ps
docker compose -p my-travel logs -f api web
```

未登录的高德代理检查应返回 `401`，不能匿名消耗服务端安全密钥。

本地 `start.sh` 显式使用非 Secure Cookie，便于在回环 HTTP 调试。生产 Compose 未
显式覆盖时默认使用 Secure Cookie，且只允许经 HTTPS 公网入口登录。

API 每次启动都会运行 `repository.Initialize`：先校验已有文档，再安装严格 validator、
受管索引和当前模型指纹。若日志出现“outside the current model”或健康检查报告数据库
契约不一致，应停止发布并在本地隔离库用完整快照定位差异；不得在服务器关闭校验器或
直接修改业务数据。

快照文件镜像补偿默认每分钟运行一次，可在受限配置中调整：

```text
SNAPSHOT_RECONCILE_INTERVAL=1m
SNAPSHOT_RECONCILE_TIMEOUT=10s
SNAPSHOT_RECONCILE_BATCH=100
```

## 服务器入口

域名可用前使用：

```text
https://<server-ip>/travel/
https://<server-ip>/travel/api/healthz
```

HTTP 只做 308 跳转到 HTTPS。宿主机 Nginx 是唯一公网入口，Web/API 不监听公网地址。
高德 JSAPI Key 应配置 IP/域名白名单；Web Service Key 只存在服务器运行配置中。

域名审批完成后，为同一路径增加域名证书和重定向，不修改前端路由、API 路径或数据库。

## 新增用户

服务器必须在权限为 `600` 的运行配置中设置 `ADMIN_PROVISION_TOKEN`。该令牌不进入 Git、
终端参数或日志，调用方以 JSON 请求体提交。新增的登录成员不会自动获得任何旅行权限；
旅行选择该基础成员后才授予 editor 权限。

本机的 `config/project.env` 设置同一令牌后，执行：

```bash
./scripts/travel-api auth create-user
```

命令会交互读取姓名、11 位手机号、令牌和两次密码；密码长度至少 6 个字符。接口路径为
`POST /travel/api/auth/users`，请求体为 `token`、`displayName`、`mobile`、`password`。
手机号全局唯一并作为登录标识；令牌为空时接口返回 403，不能创建登录成员。

空数据库也可以从本机登录成员库一次性初始化：

```bash
cp config/users.json.example config/users.json
chmod 600 config/users.json
./scripts/deploy/users.sh
```

每项同时包含 `mobile`、`password`、`memberName` 和 `admin`。手机号必须唯一且为 11 位
数字，文件中只能有一个管理员；空数据库初始化完成后，脚本会创建对应基础成员和登录
凭据。登录成员库必须保持在被 Git 忽略、权限为 `600` 或 `400` 的本机文件中；密码只
通过请求正文标准输入提交。

数据库已有管理员时，批量追加登录成员库中的所有成员：

```bash
./scripts/deploy/users.sh --provision-all
```

管理员和普通成员都可在“基础资料 → 成员”修改自己的姓名、手机号、备用联系方式、
紧急联系人联系方式和密码。修改手机号或密码需要输入当前密码，成功后现有 Session
保持有效，无需重新登录。

管理员需要为现有基础库补齐内置地点与费用类型时，可在标签页点击“补充常用类型”，或
使用同一鉴权 API 入口：

```bash
./scripts/travel-api library presets
```

该操作只增加同分类下缺失的名称，在一个事务内写入审计、提升基础库修订号并生成完整
快照；代码发布不会自动触发它。

## 重建 Nginx 入口

[`deploy/nginx.conf.example`](../deploy/nginx.conf.example) 是域名可用前的完整 IP HTTPS
模板，覆盖：

- 80 端口统一 308 跳转 HTTPS。
- `https://<server-ip>/travel/` 转发到 React Web。
- `https://<server-ip>/travel/api/` 去掉外部前缀后转发到 Go API。
- `https://<server-ip>/_AMapService/` 原样转发到需要登录会话的 Go 高德代理。
- Web/API 独立健康检查，以及反向代理和 WebSocket 所需请求头。

模板中的 `8501/8502` 是仓库默认端口。安装前先用 `ss -lnt` 或容器状态确认服务器实际
监听值；如发布配置覆盖了端口，必须在候选配置中同步修改。

先在服务器查询 IP 证书的实际名称和路径：

```bash
sudo certbot certificates
```

通常证书文件位于 `/etc/letsencrypt/live/<certificate-name>/fullchain.pem` 和
`/etc/letsencrypt/live/<certificate-name>/privkey.pem`，但必须以命令输出为准，不能
猜测目录名。私钥只留在服务器，不复制到仓库、终端记录或聊天内容。

把模板复制到服务器的临时候选文件，再替换以下三个占位符：

| 占位符 | 替换值 |
|---|---|
| `__SERVER_IP__` | 当前公网 IP |
| `__IP_CERTIFICATE_FULLCHAIN__` | IP 证书完整链的绝对路径 |
| `__IP_CERTIFICATE_PRIVATE_KEY__` | IP 证书私钥的绝对路径 |

安装前确认没有遗漏占位符，并让 Nginx 完整解析候选配置：

```bash
grep -nE '__[A-Z_]+__' /tmp/my-travel.nginx.conf
sudo cp /tmp/my-travel.nginx.conf /etc/nginx/conf.d/my-travel.conf
sudo nginx -t
sudo systemctl reload nginx
```

`grep` 应没有输出。只有 `nginx -t` 成功后才能 reload；失败时恢复原配置，不停止仍在
服务的 Nginx。当前服务器已经使用拆分的 personal gateway 配置时，应把模板中的
`map` 和两个 `server` 块合并到现有入口，不能再创建冲突的 `default_server`。

重建后验证：

```bash
curl -I http://<server-ip>/travel/
curl --fail https://<server-ip>/travel/healthz
curl --fail https://<server-ip>/travel/api/healthz
```

第一条必须返回 308，两个健康检查必须返回成功。域名审批完成后，只替换
`server_name`、HTTP 跳转目标和证书路径；`/travel/`、`/travel/api/` 与内部端口关系
保持不变。随后再次执行 `nginx -t`、reload、证书续期演练和公网检查。

## 服务器环境构建

`config/project.env` 统一保存服务器地址、SSH 用户和密钥路径、高德密钥、内部端口、
数据库名和镜像代理。真实文件不入 Git。

独立服务器设置：

```text
NGINX_MODE=standalone
TLS_CERTIFICATE_FULLCHAIN=/服务器上的证书完整链路径
TLS_CERTIFICATE_PRIVATE_KEY=/服务器上的证书私钥路径
```

已有个人共享 HTTPS 网关时设置：

```text
NGINX_MODE=shared
```

然后执行：

```bash
./scripts/deploy/environment.sh
```

脚本是幂等的，不部署项目代码，也不覆盖数据库。重建前先下载恢复包：

```bash
./scripts/deploy/backup.sh --adopt-runtime
```

`--adopt-runtime` 会把服务器已有高德密钥、初始化令牌和镜像配置写回本机受限配置，
让本机成为后续部署的配置来源；不会打印真实值。

需要重建本项目目录时：

```bash
./scripts/deploy/environment.sh \
  --reset-project \
  --backup-dir .local/server-backups/<project>/<timestamp>
```

旧应用和共享目录会移动到服务器 `recovery` 目录，不直接删除。服务器操作系统、SSH、
证书、共享网关和其他服务不在重建范围内。

## 发布前

```bash
./scripts/travel-api auth login
./scripts/travel-data pull-all
./scripts/test.sh
BOOTSTRAP_TOKEN=static-check docker compose config
git status --short
```

必须满足：

- 使用管理员会话完成整批预检；每个远程 Trip 都已有最新本地完整副本。owner Trip
  使用可编辑快照，管理员不是 owner 的 Trip 只使用完整备份中提取的
  `readonly.json`，不得用于同步或恢复。待发布数据已在本地隔离数据库完成导入、
  引用校验、快照往返和页面回归。
- 工作树干净且发布对象是确切 Git 提交。
- 服务器共享 `runtime.env`、MongoDB 数据目录和快照目录存在。
- 本机 Docker/Buildx 能为 `DEPLOY_PLATFORM` 构建候选镜像，所有 Mongo、Node、Go 和
  运行时基础镜像均固定为 `@sha256:<digest>`。
- 磁盘空间足以同时保留旧 release、候选镜像和至少一份完整数据备份。

## 发布

```bash
./scripts/deploy/project.sh local
./scripts/deploy/project.sh remote
```

本地和远程都运行同一份 `compose.yaml`：本地通过 Docker Desktop 的 Compose
启动，远程通过服务器 Docker Compose 启动。两条链路都先在本机用 Buildx 构建镜像，
再由 Compose 管理容器、网络和数据卷，不是宿主机直跑。远程发布额外把源码包 SHA-256、
镜像包 SHA-256、API/Web 镜像 ID、目标平台和基础镜像 digest 写入 `BUILD-MANIFEST`；
服务器逐项校验并 `docker load`，不会执行 `docker compose build`。脚本不指定目标时会
交互询问本地或远程，且默认保留数据库：

```bash
./scripts/deploy/project.sh remote
```

只有需要从本地完整恢复包覆盖远程数据库时，单独使用数据库脚本：

```bash
./scripts/deploy/database.sh \
  .local/server-backups/<project>/<timestamp>
```

覆盖前会再次要求输入项目级确认文字。数据库脚本只接受干净工作树，并要求服务器已经
存在与本地确切提交对应的候选 release；它在同一发布锁内备份现有数据库，精确替换目标
数据库和快照，再切换候选 release。任一步失败都会恢复发布前数据库、快照和旧 release。
低层 `project-remote.sh` 使用同一服务器锁避免并发发布，先加载本地候选镜像，再滚动更新
Web/API；已有数据库不会停止或导入攻略。新服务器没有数据库容器时，只建立空数据库，
登录成员和业务数据仍由本机初始化或恢复流程提供。已有数据库容器必须已经属于当前
Compose 的内部数据网络；拓扑不一致时发布直接停止，不会自动修改运行中的数据库网络。

发布包只来自本地 Git 的确切提交。禁止在服务器 release 中直接修前端、后端或 Compose；
禁止用 Mongo Shell 修业务数据。需要改变攻略时，回到本地
`.local/travel-data/<trip-id>/working.json`，验证后使用 `travel-data sync`。

`project-remote.sh` 不会把本地 `MONGODB_DATABASE` 写到服务器。数据库选择只保存在服务器权限
受限的 `runtime.env` 中；任何切换都必须先在隔离数据库启动同一 API、导入完整快照、
核对全部资源数量和快照往返，再单独修改服务器配置。API 启动时会验证并安装当前结构
的 validator、索引和指纹，健康检查同时确认 MongoDB 可读且结构与代码一致。

## 自动化验证

`./scripts/test.sh` 在 Docker 可用时会建立不挂载宿主机数据的一次性 MongoDB 单节点
副本集，运行 validator、索引、事务回滚、完整 HTTP 用例和快照镜像集成测试，并执行
Go 总覆盖率门槛。随后验证 OpenAPI 全部外部引用、前端类型、DOM 测试、覆盖率、生产
构建、lint 和依赖审计。真实浏览器烟测单独运行：

```bash
cd web
npm run test:e2e
```

该烟测同时覆盖桌面 Chromium 和 390px 移动视口。GitHub Actions 在 push/PR 上重复完整
检查、Go race detector 和两种浏览器视口；Actions 自身固定到确切提交 SHA。

取得当前受限快照后，还应在发布前额外执行真实语义往返；该命令只使用一次性 MongoDB，
不会写现有本地或远程数据库：

```bash
CURRENT_SNAPSHOT_DIR=.local/docker/snapshots ./scripts/test-mongo-integration.sh
```

本地 Compose 生成的完整快照位于 `.local/docker/snapshots`；通过 `travel-data pull-all`
取得的副本也可以改用 `.local/travel-data`。测试会递归发现完整快照，但每个 Trip 只选择
`exportedAt` 最新的一份；时间相同再比较 `revision`，身份仍相同时优先本地
`working.json`，使未重算校验和的本地编辑直接触发门禁而不会静默回退到 `base.json`。
测试以当前 validator 和索引写入隔离数据库，再重新生成快照并比较内容校验和。基础库也只
选择同样规则下的最新一份，并由每个 Trip 的隔离数据库和基础库自身往返共同使用；缺少
基础库时门禁直接失败，从而同时验证旅行成员、标签和记录操作者的外部引用。管理员可移植
备份原件使用无 `.json` 扩展的
`admin-backup`，不会被误识别为快照；离线工具先重算备份校验和、核对依赖清单和内嵌
快照，测试只读取校验后单独保存的 `readonly.json`。完整数据库恢复演练仍是远程结构
发布前的最终门槛。

## 发布后

```bash
curl -I http://<server-ip>/travel/
curl --fail https://<server-ip>/travel/ >/dev/null
curl --fail https://<server-ip>/travel/healthz
curl --fail https://<server-ip>/travel/api/healthz
```

还需人工验证：

- 登录和会话续期。
- 默认日期、前后日切换及刷新恢复。
- 当天地点顺序、地图交通方式与导航链接。
- 天气的报告时间、缓存状态和失败降级。
- 新增弹层在桌面与 390px 移动端保持上下文。
- 攻略资源数量与当前数据模型一致。

## 故障恢复

代码故障优先回切上一 release；数据故障使用 API 的 restore preview/apply：

1. 确认故障只影响代码还是已经影响数据。
2. 代码问题回切 release，不操作 MongoDB。
3. 数据问题选择已校验完整快照，先预览差异。
4. 保存当前故障状态快照，再由 owner 确认恢复。
5. 验证引用完整性、快照校验和和公网核心流程。

任何恢复都不得删除灾备数据库或历史快照来“清理现场”。
