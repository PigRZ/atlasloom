---
name: flyai
description: Use when a travel itinerary needs candidate flights, trains, hotels, points of interest, or broad travel search through the FlyAI command-line tool. Treats results as time-sensitive leads, never books or pays, and requires final verification with the operator before itinerary import.
---

# 飞猪 FlyAI 旅行候选

使用 FlyAI CLI 找交通、住宿和 POI 候选。它只负责搜索；价格、余票、退改和可预订状态
必须在用户实际下单前重新确认。

## 安装检查

```bash
command -v flyai
flyai --help
```

缺失时由用户自行确认后安装当前项目验证过的版本：

```bash
npm install -g @fly-ai/flyai-cli@1.0.16
```

不要猜测参数；先读取相关子命令帮助。不要把平台 Cookie、账号或支付信息写入项目。

## 常用只读搜索

```bash
flyai search-flight --origin "出发城市" --destination "到达城市" \
  --dep-date "2026-10-01"

flyai search-train --origin "出发城市" --destination "到达城市" \
  --dep-date "2026-10-01"

flyai search-hotel --dest-name "目的地" \
  --check-in-date "2026-10-01" --check-out-date "2026-10-03"

flyai search-poi --city-name "目的地" --keyword "博物馆"
flyai ai-search --query "固定日期、人数、预算和硬约束组成的完整问题"
```

## 写入攻略的规则

- 使用明确日期、出发地、目的地、人数、舱位或床型和预算，避免宽泛查询。
- 候选记录查询时间、平台、班次或酒店标识、价格口径、退改条件和原始链接（能取得时）。
- 不把搜索结果标记为已预订；计划中的状态最多是 `needs_booking`。
- 与高德的地点、行车距离和 FlyAI 的交通住宿结果交叉核对，冲突时保留冲突并请求用户
  选择。
- 不执行登录、预订、锁座、支付、退订或向外部平台提交个人信息。
