# 使用 AI 创建旅行

AI 工作流分为两件事：

1. 调研并写出有来源、有限制和 Plan B 的攻略。
2. 把用户确认过的攻略转换为当前模型旅行，经预览后导入。

联网内容、模型输出和平台搜索都不直接写数据库。唯一写入边界是现有旅行备份导入 API，
且必须经过严格编译、服务端预览、基础资料映射和人工确认。

## 组件

| 项目级 Skill | 作用 | 外部依赖 |
|---|---|---|
| `$agent-reach` | 网页、代码、社交平台与视频等只读调研 | Agent Reach CLI、所选平台依赖/登录态 |
| `$amap-lbs-skill` | 地点、坐标、路线、天气 adcode | Node 18+、高德 Web Service Key |
| `$flyai` | 航班、火车、酒店、POI 候选 | Node/npm、FlyAI CLI |
| `$travel-guide` | 编排调研、攻略、计划、预览导入 | 上述三项、Go 1.26 |

这些 Skill 已放在 `.agents/skills/`。用支持 Agent Skills 的客户端打开仓库后若未识别
新 Skill，重新打开仓库或重启客户端会话。Skill 文档和脚本可以公开，登录态和 Key
不能公开。

## 1. 安装 Agent Reach 与小红书后端

推荐用 pipx 隔离安装 Agent Reach：

```bash
pipx install https://github.com/Panniantong/agent-reach/archive/main.zip
agent-reach install --env=auto --channels=opencli,xiaohongshu
agent-reach doctor --json
```

小红书桌面检索优先使用 OpenCLI，需要用户在自己的 Chrome 安装对应扩展并完成登录。
建议使用专门的低权限账号。不要把 Cookie、二维码或会话文件复制到项目、聊天或命令行
参数。

体检结果必须显示小红书存在可用 `active_backend`。没有登录、触发验证或平台不可用时，
攻略要明确写“未取得小红书结果”，改用官方网页和公开搜索，不能伪造笔记。

## 2. 安装 FlyAI

```bash
npm install -g @fly-ai/flyai-cli@1.0.16
command -v flyai
flyai --help
```

版本号是本项目当前验证基线。升级前先核对命令参数和输出兼容性。FlyAI 只找候选，不
登录、预订、锁座或支付。

## 3. 配置高德

在被忽略且权限为 `600` 的 `config/project.env` 填：

```text
AMAP_WEBSERVICE_KEY=<your-web-service-key>
```

检查脚本帮助和一次无敏感输出的查询：

```bash
node .agents/skills/amap-lbs-skill/scripts/amap.mjs place \
  --keywords "示例博物馆" --region "示例市"
```

Key 只从环境或 `config/project.env` 读取，不接受命令参数。坐标顺序是
`经度,纬度`。

## 4. 向 `$travel-guide` 提需求

推荐提示：

```text
使用 $travel-guide 为我规划一场旅行。
日期：2026-10-01 至 2026-10-04
出发地：示例市
目的地：示例目的地
成员：成员甲、成员乙
预算：……
交通偏好：……
必去 / 不去：……
体力、海拔、饮食或其他硬约束：……

先调研并给我确认攻略，不要直接导入。
```

Skill 会在被忽略的目录建立：

```text
.local/ai-trips/<trip-slug>/
  brief.md
  sources.md
  guide.md
  plan.json
  trip-backup.json
```

目录权限为 `700`，文件为 `600`。使用成员别名；不要在 brief 或攻略中放手机号、证件、
账号、Cookie、Token、家庭住址和完整订单资料。

## 5. 审查攻略

`guide.md` 至少应讲清：

- 每天的地点顺序、停留和交通。
- 住宿区域与交通/住宿候选的查询时间和价格口径。
- 预约、购票、开放时间、天气、海拔、道路和安全限制。
- 预订清单、费用假设、物品负责人。
- 每个关键失败点的 Plan B 和退出条件。
- 来源 URL、发布日期或未知、检索时间、可信度和待官方复核项。

小红书是经验来源，不是最终事实。班次、票价、路况、开放政策和天气必须尽量用官方来源
确认。来源冲突时应并列呈现，交给用户选择。

只有用户确认路线骨架后才进入计划编译。

## 6. 生成并编译 plan.json

字段说明见项目 Skill 的
`.agents/skills/travel-guide/references/plan-schema.md`，可复制
[示例计划](examples/ai-trip-plan.example.json)。

计划的关键不变量：

- 日期范围内每天恰好一个 Day。
- `placeEntries` 顺序是当天执行顺序。
- Segment 的起终点都属于该 Day。
- 标签分类正确，成员/地点/预订等引用不悬空。
- 每个预订恰好有一笔关联的待登记费用。
- Booking 必须填写费用类型 `typeTagKey` 和预订人 `bookerMemberKey`。
- 关联费用的 `item`、`typeTagKey`、`payerMemberKey` 与对应 Booking 完全一致；后两项
  可以在关联费用中省略并由编译器从 Booking 派生。
- 每件物品都有指向 `members[]` 的 `ownerMemberKey`。
- 金额使用最小货币单位；未知金额不虚构。
- 攻略中的约束、Plan B、状态、负责人和来源摘要没有被静默丢弃。

编译：

```bash
./scripts/travel-plan build <trip-slug>
```

该命令只读取固定的 `.local/ai-trips/<trip-slug>/plan.json`，离线生成
`trip-backup.json`。它拒绝未知字段、缺失日期、无效枚举、错误引用、未使用标签和任何
不符合当前旅行备份契约的内容；输出不会覆盖已有文件。

## 7. 预览与导入

先启动系统并登录 API：

```bash
./scripts/travel-api auth login
./scripts/travel-api trip-imports preview \
  .local/ai-trips/<trip-slug>/trip-backup.json
```

这一步只读。推荐随后在 Web 完成导入：

1. 进入“所有旅行”→“导入旅行”。
2. 选择 `trip-backup.json`。
3. 核对名称、日期、各资源数量和校验和。
4. 至少把代表发起人的来源成员映射到当前登录成员；其他来源成员映射到真实基础成员，
   或明确创建无登录凭据成员。
5. 将标签映射到已有标签，或明确让导入事务创建缺失标签。
6. 确认不同来源对象没有错误合并。
7. 用户明确同意后点击确认导入。

导入在一个事务内创建全新 Trip，不覆盖原旅行；导入者始终成为 owner，其他导入成员
成为 editor。失败不会留下半个旅行。

## 8. 导入后验收

- 桌面和手机分别打开每一天，检查顺序、时间和溢出。
- 地图点位、行政区、导航链接和路线方向正确。
- 成员、支付人、分摊和资料记录人映射正确。
- 在计划和导入预览中核对物品的 `ownerMemberKey` 映射；导入后由各登录成员分别确认
  自己可见的物品，普通物品页面不会展示他人物品。
- 预订仍是实际状态，关联费用的项目、类型、支付人完全一致且没有虚构金额。
- 资料保留来源、限制、Plan B 和待复核项。
- 由 Trip owner 或管理员从旅行总看板导出一次新备份并保存到受限目录。该完整备份
  包含所有成员的私有物品，只用于恢复或灾备，不作为日常查看入口。

远程实例同样只通过 HTTPS API 执行预览和导入。禁止 SSH 改 release、MongoDB 或业务
数据；代码发布与这次数据导入仍是两个独立动作。

## 更新依赖

依赖升级后至少执行：

```bash
agent-reach doctor --json
flyai --help
node .agents/skills/amap-lbs-skill/scripts/amap.mjs --help
./scripts/test.sh
```

如果 CLI 参数或平台登录方式变化，先更新项目级 Skill 和本手册，再让其他用户使用。
