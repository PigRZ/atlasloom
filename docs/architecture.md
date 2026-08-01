# 系统架构

## 产品结构

个人旅行看板以日期为主线，地点是日程的直接内容：

```text
基础库
  ├─ Member + 可选手机号登录凭据
  └─ Tag
       ├─ 通用
       ├─ 地点类型
       ├─ 费用类型
       └─ 平台类型

Trip ──> 选择 Member
  ├─ Day ──> ordered Place
  │            └─ Segment: Place A → Place B
  ├─ Booking ──> Place + Platform + expense type + booker ──自动生成→ Expense
  ├─ Expense ──> payer + frozen split members
  ├─ PackingItem ──> current Member + Day
  └─ ResearchEvidence ──> Platform + recorder + Day/Place
```

Day 直接关联地点。Booking 保存预订事实、费用类型和预订人并可选关联地点；自动费用的
项目、类型和支付人由 Booking 强制同步，金额与分摊在 Expense 中登记。用餐支出使用
“三餐”费用类型。天气由 Day 中地点的行政区代码组成列表。

登录后的页面结构：

```text
所有旅行 /travel/app/trips
  ├─ 导出某次旅行的当前完整业务备份
  └─ 预览成员/标签映射后导入为新旅行
基础资料 /travel/app/library
  ├─ 成员
  └─ 标签

某次旅行
  ├─ 今天
  ├─ 日程
  ├─ 地图
  ├─ 费用
  └─ 更多：地点、预订、物品、成员、资料、操作记录
```

## 运行链路

```text
Browser
  ├─ /travel/      → React
  └─ /travel/api/  → Go HTTP API
                         ↓
                      service
                         ↓
                    repository
                         ↓
                      MongoDB transaction
                         ├─ business data
                         ├─ immutable audit log
                         └─ complete snapshot
                                  ↓
                            JSON file mirror
                                  ↑
                         periodic retry worker
```

- React 只持有会话与短期 UI 状态。
- Go API 负责认证、成员启停与 Session 撤销、已停用无引用成员的受控凭据注销、
  管理员校验、Trip 权限与逻辑删除、物品 owner 过滤、手工费用与匿名个人物品的成员
  级联删除、引用校验、幂等与并发控制。预订、预订生成费用和资料引用不会被自动删除。
- 所有用户发起的业务写入都在 MongoDB 事务回调内重新读取当前成员状态和所需
  admin/Trip 角色；事务因并发冲突自动重试时也会重新授权，不沿用 HTTP 入口的旧判断。
- 每条 Trip 资源记录保存创建成员和最近修改成员，作为进入完整快照的当前元数据；同一
  事务还会追加不可变操作记录，保存每次动作及修改前后内容。恢复快照只替换当前元数据，
  不覆盖操作历史。
- Trip 概览按当前成员统计物品；Trip 操作记录不混入基础库日志，并过滤其他成员的物品
  变更。共享 Trip 资源可按各自分组排序；个人物品没有共享顺序，排序接口明确拒绝该资源。
- 管理员跨旅行能力只包含完整备份导出和逻辑删除；它不会绕过 Trip 的查看、编辑权限。
- 成员 ID 是登录、权限与业务关系的唯一身份键；密码哈希只在私有凭据集合。
- MongoDB 是运行时唯一数据源。JSON 文件是已提交快照镜像，不反向参与在线读取。
- API 启动时先以当前模型校验全部已有文档，再安装严格集合 validator、受管索引和模型
  指纹；任何不符合当前契约的文档都会阻止启动，不会被静默改写。健康检查同时核对代码
  与数据库指纹。
- 文件镜像失败时数据库快照保持 `pending`；启动扫描和周期任务都会重试 Trip 与基础库
  快照，重试批量、周期和超时由运行配置控制。
- Trip 快照与可移植备份包含所有成员物品，作为完整恢复与管理员灾备的权限例外；快照
  入口统一要求旅行 owner，可移植备份只允许 owner 或管理员导出，editor 和 viewer
  不能读取完整 payload。
- 用户下载的旅行备份是可移植文件，不是运行时数据源；导入时必须经校验、依赖映射和
  新身份分配后才能进入 MongoDB。

## 前端边界

- `web/src/app/`：路由与导航。
- `web/src/features/day/`：日期选择、日程地点编辑、地点天气和地图。
- `web/src/features/records/`：地点、预订、费用、物品、资料与操作记录。
- `web/src/features/library/`：管理员维护成员与分类标签；成员可修改自己的联系方式和密码。
- `web/src/components/`：页面内确认框、抽屉和通用状态。
- `web/src/lib/`：API、类型和纯计算模型。

当前日期写入 `/travel/app/trips/{tripId}/day/{YYYY-MM-DD}`。桌面与移动端共享同一信息
结构；桌面使用侧栏和居中内容区，移动端使用底部一级导航与底部弹层。

首次创建旅行后直接进入“日程”。服务端已经按日期范围生成 Day 骨架，页面引导用户从
第一天创建地点；当天入口的地点创建会在服务端原子地完成 Place 创建与 Day 关联。成员、
预订、费用等后续页面在依赖缺失时先展示具体准备项，不打开无法完成的空表单。

## 后端边界

Go 主模块使用项目内中立命名空间 `my-travel/server`，内部 import 不依赖代码托管平台
的用户、组织或远端地址。仓库无论直接克隆还是 Fork，均使用同一套源码与构建路径。

- `httpapi`：HTTP、会话、CSRF、Trip 授权和错误响应。
- `service`：事务用例、自动费用、成员访问权和操作记录；快照导入和镜像补偿单独成文件。
- `repository`：Mongo 查询、当前数据库契约、引用校验与快照；聚合查询和快照仓库分文件。
- `model`：当前唯一领域模型。
- `snapshot`：提交后文件镜像。

浏览器、脚本和测试都以 `api/openapi.yaml` 为接口入口。根文件只聚合模块：
`api/paths/` 按 system、auth、library、trips、records、snapshots、backups、maps 拆分
路径，`api/components/` 保存对应响应与模型。Go 契约测试会递归解析外部 `$ref`，并要求
每个操作只有一个模块标签、成功响应具有明确 JSON schema。

## 地图与天气

- 地图使用高德 JSAPI；安全密钥由同源固定一级代理注入。
- 地点搜索和逆地理编码由 Go API 使用服务端 Web Service Key。
- 驾车、出租车、网约车、步行和骑行按真实路径规划；火车、飞机等使用明确示意线。
- “在高德打开”只依据地点坐标和交通方式生成链接，不区分乘客或司机。
- 天气接口带缓存时间、新鲜度和失败状态。“在途”模式只请求当前选中地点；“规划”模式
  不发起天气请求。

外部服务失败时，日程、地址、坐标、既有导航链接和缓存天气仍可读。

## 部署

```text
Internet → Nginx :80/:443
                 ├─ 127.0.0.1:<APP_PORT> → Web
                 └─ 127.0.0.1:<API_PORT> → API
                                                   ↓
                                             private Docker network
                                                   ↓
                                                MongoDB
```

本地 Docker 是唯一开发与结构验证环境。远程只接收本地验证过的确切提交、当前数据库结构
和经预览确认的数据快照；部署项目默认不覆盖数据库。API/Web 镜像在本地按目标平台构建，
基础镜像必须固定 SHA-256 摘要；服务器只校验制品摘要和镜像 ID 后加载，不重新构建。
