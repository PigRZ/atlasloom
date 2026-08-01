---
name: travel-guide
description: Use when the user wants AI to research a trip, write an evidence-backed Chinese guide, or initialize a new my-travel trip from that guide. Orchestrates project-local Agent Reach, Amap, and FlyAI skills, pauses for itinerary approval, compiles a strict offline plan, and imports only through preview and explicit confirmation.
---

# AI 旅行攻略与初始化

把“网上资料”逐步变成可审查攻略和可导入旅行。任何阶段都不得直接写 MongoDB，也不得
绕过当前 API、备份校验和用户确认。

## 先读

- [AI 计划字段](references/plan-schema.md)
- [攻略模板](references/guide-template.md)
- 项目根目录 `docs/ai-trip.md`
- 涉及模型字段时再读 `docs/data-model.md`

## 工作流

### 1. 建立受限工作区

从用户给出的目的地和日期生成小写 `trip-slug`，只在以下目录工作：

```text
.local/ai-trips/<trip-slug>/
  brief.md
  sources.md
  guide.md
  plan.json
  trip-backup.json
```

目录权限设为 `700`，文件设为 `600`。如果目录已经存在，先读取并复用，不覆盖旧文件。
真实姓名、手机号、证件、账号、Cookie 和 Token 不写入这些产物；成员先使用稳定别名。

### 2. 明确约束

至少取得日期、出发地、目的地、人数/成员别名、交通偏好、预算、体力或健康限制和必去/
不去项。缺失但不影响路线骨架的内容可以标为“待确认”，不能凭空补全。

### 3. 调研

1. 用 `$agent-reach` 读取小红书、公开网页和其他适用平台的旅行经验。
2. 用 `$amap-lbs-skill` 核验地点、坐标、行政区、路线距离与天气 adcode。
3. 用 `$flyai` 搜索航班、火车、住宿和 POI 候选。
4. 官方来源优先确认预约、开放时间、价格、班次、道路和安全限制。

所有来源写入 `sources.md`，包含 URL、发布日期或未知、检索时间、支持结论、可信度和
是否仍需官方复核。

### 4. 先交付攻略，再请求确认

按 [攻略模板](references/guide-template.md) 写 `guide.md`。同时在对话中展示路线摘要、
重大取舍、硬约束、Plan B 和待确认项。用户没有确认路线骨架前，不生成可导入备份。

### 5. 生成严格计划

根据已确认攻略写 `plan.json`：

- 日期范围内每天恰好一个 Day。
- `Day -> Place` 的顺序和 `Segment` 起终点完全一致。
- 高德坐标使用 `经度,纬度` 核验后分别写入 longitude/latitude。
- 资料原文转为 `researchEvidence` 摘要和链接；不丢失限制、Plan B、状态和负责人语义。
- 未预订项目状态为 `needs_booking`，不虚构价格、余票或实时天气。
- 每个预订都带一笔通过 `bookingKey` 关联的待登记费用；未知金额和支付人保持空缺。
- 每个标签、成员和跨记录引用都使用稳定 key，不使用展示名称作外键。

随后运行：

```bash
./scripts/travel-plan build <trip-slug>
```

编译器会拒绝未知字段、缺失日期、悬空引用、错误标签分类、未使用定义和不符合当前模型
的记录，并以 `600` 权限创建 `trip-backup.json`；它不会连接数据库。

### 6. 预览并导入

1. 可先运行 `./scripts/travel-api trip-imports preview <backup-file>` 做只读 API 预览。
2. 推荐登录 Web，进入“所有旅行”→“导入旅行”，选择 `trip-backup.json`。
3. 在预览中至少把发起人别名映射到当前登录成员，再处理其他成员；标签可映射到已有
   标签或在导入事务中创建。
4. 展示旅行名称、日期、资源统计和校验和，取得用户明确确认后才点“确认导入”。
5. 导入完成后抽查日程顺序、地点坐标、成员、费用、预订和资料，并导出一次新备份。

远程实例同样只通过 HTTPS API 预览和导入；不得 SSH 修改代码、结构或业务数据。

## 失败语义

- 检索平台不可用：保留失败状态，改用可验证来源，不冒充成功。
- 事实冲突：并列来源、标出时间差，交给用户选择。
- 编译失败：修改 `plan.json` 后重新编译；已有备份不覆盖，先人工归档或删除确认。
- 预览失败：不执行导入，修复依赖或模型错误后重新生成。
- 导入失败：系统事务不会留下半个旅行；保留本地攻略和备份供重试。
