# 真实数据工作流

## 本地优先的两条独立链路

这个项目的代码仍在开发，但旅行攻略是真实资产。两者不能使用同一个发布动作：

```text
Remote data ──> pull-all ──> local copy and isolated rehearsal
                              ├─> Git commit ──> test ──> deploy code
                              └─> edit snapshot ──> diff ──> sync data
```

- `deploy` 只更新代码和无密钥配置结构，不读取或导入攻略。
- `sync` 只通过鉴权 API 修改一个 Trip，不重建容器、不 SSH 写 MongoDB。
- 服务器 MongoDB 是线上运行时真相，但不是开发入口。每轮远程变更开始时，先把全部
  Trip 的最新完整快照复制到本地，之后所有代码、结构和数据改动都从本地验证结果发布。
- 本地快照是该轮变更的唯一编辑源；远端若发生并发变化，本地同步必须失败并重新拉取，
  不能在服务器上手工合并。
- 本地 `MONGODB_DATABASE` 只服务本地开发；发布脚本不会把它传播到服务器。服务器切换
  运行数据库必须作为一次有备份、有候选验证和明确确认的独立运维操作。

## 修改攻略

```bash
./scripts/travel-api auth login
./scripts/travel-data pull-all
./scripts/travel-data status <trip-id>

# 编辑命令输出的 working.json

./scripts/travel-data diff <trip-id>
./scripts/travel-data sync <trip-id>
```

`pull` 以旅行 owner 身份保存完整快照、远端聚合 `version` 和校验和；editor 与 viewer
不能列出、下载或预览快照。`sync` 先做只读预览，仅在以下条件同时满足时允许提交：

1. 本地快照通过全部领域校验。
2. Trip ID 与工作目录一致。
3. 当前远端修订仍等于本地拉取时的基准。
4. 用户确认预览中的资源新增、修改、删除数量，以及 `membershipAccess` 中稳定排序的
   成员访问权新增和撤销。
5. 删除资源或撤销成员访问权时显式传入删除确认选项；两者都计入 `deleteCount`。
6. 服务端事务内生成同步前和同步后完整快照。

若其他设备先改了远端数据，sync 必须失败；重新 pull 后人工合并，不能用“最后写入”
覆盖真实攻略。

`pull-all` 会先读取会话管理员状态、全部旅行角色并检查已有 `working.json`，在任何
本地文件替换前完成整批预检。owner 旅行继续生成可编辑的 `base.json` 和
`working.json`；管理员对自己不是 owner 的旅行只调用允许的完整备份导出，将可移植
JSON 保存为无 `.json` 扩展的 `admin-backup`。离线工具会按导入契约重算完整备份
校验和、核对依赖清单并验证内嵌快照，再把该快照保存为 `readonly.json`。该工作区标记
为 `admin-backup`，`status` 显示
`read-only-backup`，`diff`、`sync` 和 `restore` 都会在本地直接拒绝，不会调用预览或
写入接口。角色切换后重新拉取会移除上一种模式的当前工作文件，避免旧
`base.json`/`working.json` 被误当成可编辑数据。

非管理员 editor/viewer 无权取得包含全体成员物品的完整载荷，因此整批预检会失败且不
替换基础库或任何旅行文件。发布前需要覆盖全部活动旅行时应使用管理员账号；这只复用
其既有完整备份导出权限，不会授予快照查看、恢复或资源写入权限。基础库快照位于
`.local/travel-data/library/base.json`；全部完整快照均被忽略且权限受限，不得提交 Git。

## 用户旅行备份

旅行负责人或管理员可在“旅行总看板”导出某次旅行的当前完整业务状态，也可以选择同格式 JSON，
先预览资源数量和基础资料映射，再生成一个全新的旅行。

备份只携带当前 Trip 状态和实际引用的成员、标签描述，不携带基础库全集、登录凭据、
管理员身份、历史快照或操作记录。导入不会覆盖原旅行；所有旅行内 ID 都重新生成。
同一部署中的成员按稳定 ID 自动匹配，标签还可按“分类 + 名称”建议匹配。缺失或冲突
必须由用户明确选择；管理员可显式创建无登录凭据成员或标签。标签同名但分类不同时不会
跨分类匹配，而是再次询问是否在正确分类创建；同一分类同名标签由服务端唯一约束阻止
重复创建。导入者始终成为新旅行 owner。

命令行可执行：

```bash
./scripts/travel-api trips backup <trip-id> [output-file]
./scripts/travel-api trip-imports preview <backup-file>
# apply 请求文件包含 backup、confirmChecksum、memberMappings、tagMappings
./scripts/travel-api trip-imports apply @<request-file>
```

旅行备份用于用户级复制和恢复当前业务状态。服务器灾难恢复仍使用完整 MongoDB 灾备，
二者不能互相替代。完整备份包含这次旅行中所有成员的物品，是普通成员物品查询权限之外
的恢复与灾备例外；只能由旅行 owner 或管理员导出，不能提供给 editor 或 viewer。

AI 攻略初始化使用完全相同的备份边界：项目级 `$travel-guide` 先生成并由用户确认攻略，
再让 `scripts/travel-plan` 把严格 `plan.json` 离线编译为旅行备份。编译器不连接
MongoDB；导入仍通过 `trip-imports preview/apply`，不会获得另一条数据写入入口。完整
步骤见 [使用 AI 创建旅行](ai-trip.md)。

## 代码发布

```bash
./scripts/travel-data pull-all
./scripts/test.sh
BOOTSTRAP_TOKEN=static-check docker compose config
git commit
./scripts/deploy/project.sh remote
```

发布脚本从干净的确切提交在本机构建目标平台候选镜像，记录并校验源码包、镜像包、
镜像 ID 和固定基础镜像 digest；服务器只加载该制品，再滚动替换 Web/API，不重新构建。
替换后的健康检查失败时自动回滚到旧 release。MongoDB 与共享快照目录在发布全程保持
运行。

服务器不是调试环境：不得 SSH 修改 release 内的前端、后端或脚本。数据库结构只允许
由本地已验证的当前模型和候选数据库演练进入线上；业务数据只允许由本地
快照经 API 预览后同步。即使同一次产品改动同时涉及代码和数据，也必须分别发布、分别
验收和分别回滚。

## 数据结构发布

当前运行时只支持 `docs/data-model.md` 描述的一套模型，不保留双读或双写。

结构变化的安全步骤：

1. 只读拉取远端完整快照和数据库灾备。
2. 在本地同时更新当前模型、接口、页面、脚本和数据快照。
3. 用语义清单核对各资源数量、时间、链接、坐标、成员和费用关系。
4. 在隔离数据库恢复并运行 API、快照往返和前端回归。
5. 需要替换服务器数据库时，只使用独立的数据库覆盖脚本和已验证恢复包；先让项目部署
   构建与本地提交一致的候选 release，再由数据库脚本在发布锁内完成精确替换和联动切换。
6. 公网验收后保留发布提交、恢复包和验证结果。

API 和前端只读取当前字段。

## 恢复

```bash
./scripts/travel-data restore-preview <trip-id> <snapshot-file>
./scripts/travel-data restore <trip-id> <snapshot-file>
```

恢复预览和提交都要求 owner 身份，并显示将新增、修改和删除的对象；提交还要求快照
校验和及明确确认。恢复历史快照时请求体必须显式携带 `allowDelete`，预览中的
`deleteCount` 大于零时只有 `allowDelete=true` 才能继续；服务端保存恢复前快照。不要
直接复制文件到容器，也不要 SSH 修改集合。快照导入和恢复都不得改变当前 Trip 的
`deletedAt` 或 `deletedByMemberId`；旅行逻辑删除只能通过独立删除接口执行，删除旅行的
历史快照仍可校验和用于灾备。
