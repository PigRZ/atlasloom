# AI 旅程计划字段

`plan.json` 是便于人和 Agent 编辑的输入，不是运行时数据库结构。唯一转换入口是：

```bash
./scripts/travel-plan build <trip-slug>
```

完整示例见 `docs/examples/ai-trip-plan.example.json`。

## 顶层

```text
trip
tags[]
members[]
days[]
places[]
segments[]
bookings[]
expenses[]
packingItems[]
researchEvidence[]
```

所有数组即使为空也建议显式写出。未知字段会被拒绝。

## 稳定 key 与引用

- `key` 只能包含字母、数字、点、下划线、冒号和连字符，不能含 `..`。
- 同一数组的 key 唯一。显示名称随时可改，引用只能使用 key。
- `placeKey`、`dayKey`、`memberKey`、`tagKey`、`bookingKey` 必须指向已定义对象。
- 日期使用 `YYYY-MM-DD`，时间使用 `HH:MM`，本地日期时间使用
  `YYYY-MM-DDTHH:MM`。
- 金额使用最小货币单位整数，例如人民币 128.00 元写 `12800`。

## trip

必填：`id`、`title`、`startDate`、`endDate`。可选：`summary`、`timezone`、
`currency`。`summary` 用一段文字概括路线主题和全程提醒；时区与币种缺省时使用
`Asia/Shanghai`、`CNY`。

## tags 与 members

标签：

```json
{"key":"attraction","name":"景点","category":"place-type","color":"#1677FF"}
```

`category` 只能是 `place-type`、`expense-type`、`platform-type`，且必须真的被记录
引用。成员：

```json
{"key":"traveler-a","displayName":"成员甲","note":""}
```

至少一个成员。计划不指定最终权限；导入者始终成为 owner，其余导入成员成为 editor。
导入预览时至少把代表发起人的来源成员映射到当前登录成员；其他成员可映射到真实基础
成员或创建无登录凭据成员。

## days、places、segments

- 日期范围内每天恰好一个 Day；`placeEntries` 的数组顺序就是当天执行顺序。
- Place 必须有 `typeTagKey`；经纬度要成对出现。
- Segment 的 `dayKey` 指向一个 Day，起终点必须都已出现在该 Day 的
  `placeEntries`。
- `mode` 可用：`walk`、`drive`、`taxi`、`rideshare`、`public_transit`、`train`、
  `flight`、`shuttle`、`scenic_bus`、`ferry`、`bicycle`、`other`。

## bookings 与 expenses

- Booking 必填 `item`、费用类型 `typeTagKey` 和预订人 `bookerMemberKey`；状态可用
  `needs_booking`、`booked`、`cancelled`。
- 平台标签必须是 `platform-type`。
- 每个 Booking 必须恰好有一笔 Expense 通过 `bookingKey` 关联，保持系统“预订对应待
  登记费用”的不变量。
- 通过 `bookingKey` 关联的 Expense，其 `item` 必须与 Booking 的 `item` 完全一致；
  `typeTagKey` 和 `payerMemberKey` 可以省略并由 Booking 派生，显式填写时必须分别与
  Booking 的 `typeTagKey` 和 `bookerMemberKey` 完全一致。
- 手工 Expense 必填 `item`、`typeTagKey`、`payerMemberKey`、`splitMode`、
  `splitMemberKeys`；关联预订的 Expense 仍必填 `item`、`splitMode`、`splitMemberKeys`。
- `splitMode` 为 `selected` 或 `all`；冻结的分摊成员数组不能空。
- 通常填写正整数 `amountMinor`。绑定预订且价格未知的费用可以省略金额，支付人始终
  来自预订人；不能虚构未知金额。

## packingItems 与 researchEvidence

- 每件物品必填 `key`、`item` 和 `ownerMemberKey`；`ownerMemberKey` 必须指向
  `members[]` 中的成员，编译器会将它解析为备份中的 `ownerMemberId`。
- 物品分类可用：`clothing`、`document`、`medicine`、`electronics`、`daily_use`、
  `supply`、`gear`、`vehicle`、`other`。
- 物品的 `category`、`dayKeys`、`quantity`、`packed` 和 `notes` 可选；
  `dayKeys` 可以关联多个执行日。
- 导入时成员映射决定物品最终所属的基础成员。导入后的普通物品接口只向所属成员返回
  该物品；完整旅行备份包含所有成员物品，只能按恢复与灾备边界处理。
- 资料保存标题、摘要内容、平台、记录成员及可选执行日/地点；URL 放在 `content` 中。
- 攻略中的硬约束、Plan B 和待复核项应进入相关 Day、Place、Booking 或
  ResearchEvidence，不能只留在 Markdown 中。
