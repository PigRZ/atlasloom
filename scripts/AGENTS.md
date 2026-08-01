# 脚本约束

这里提供本地启动、全量验证、真实攻略、服务器环境构建和项目部署入口。

- 所有脚本使用 `set -euo pipefail`，从自身位置解析仓库，不依赖调用目录。
- 仓库身份只由脚本所在目录确定；不得硬编码代码托管平台用户、组织、远端 URL 或本机
  绝对路径。
- 目标服务器和 API 必须来自 `config/project.env`；仓库与帮助文本只使用文档示例 IP。
- 密码和 bootstrap token 交互读取；请求正文走 stdin；Cookie、CSRF、快照和临时状态只
  写入被忽略且权限受限的目录。
- `travel-api` 是受测试的低层 API client；`travel-data` 编排 pull、diff、sync 和
  restore，不得用循环 CRUD 冒充原子同步。
- 数据同步只能走 owner 鉴权 API，必须校验基准修订、校验和、删除确认及服务端预览。
  禁止 SSH 写 MongoDB。
- 远程变更采用本地优先顺序：先 `travel-data pull-all` 保存全部远端快照，再在本地
  验证代码、结构和数据；服务器只接收本地确切提交与经预览确认的快照同步。
- 部署相关脚本集中放在 `scripts/deploy/`：`environment.sh` 只建立系统依赖、目录、
  运行配置和 Nginx；`project.sh` 选择本地或远程并只部署项目；`database.sh` 单独负责
  数据库覆盖。项目部署始终保留数据库，数据库脚本必须有本地恢复包和二次确认。
- 初始化登录成员只读取被忽略且权限为 600 的 `config/users.json`。明文密码不得进入
  参数、日志、发布包或服务器运行配置，只能通过请求正文标准输入提交。
- 发布只更新干净的确切 Git 提交和明确配置；API/Web 必须在本机通过 Buildx 按
  `DEPLOY_PLATFORM` 构建，基础镜像固定 digest。源码包与镜像包摘要、镜像 ID 和目标
  平台写入清单，服务器校验后只执行 `docker load` 和 `compose up --no-build`。
  发布不停止 MongoDB、不导入攻略，失败时恢复上一 release 和运行配置。
- 项目启动脚本不修整业务文档或切换数据结构；启动前目标数据库必须已经满足代码声明的
  当前严格契约，否则保留失败现场并停止发布。
- `test-mongo-integration.sh` 必须使用一次性单节点副本集和固定 digest，不挂载现有
  数据。数据库变化还要通过 `CURRENT_SNAPSHOT_DIR` 对当前完整快照做 validator、索引、
  事务和语义往返验证。
- 脚本增删命令或配置项时同步 README、`docs/data-workflow.md`、
  `docs/operations.md` 和对应测试。
- 修改后至少运行 `bash -n`、相关脚本测试及根目录 `./scripts/test.sh`。
