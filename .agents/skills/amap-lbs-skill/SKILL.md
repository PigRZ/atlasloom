---
name: amap-lbs-skill
description: Use when a China-focused travel plan needs verified Amap place search, coordinates, reverse geocoding, route estimates, or weather data. Reads the Web Service key from the environment or ignored project configuration, never from arguments, and returns raw JSON for evidence-based itinerary work.
---

# 高德旅行地理信息

使用 `scripts/amap.mjs` 查询中国大陆地点、坐标、路线和天气。高德返回值是外部数据，
必须结合检索时间和失败状态使用。

## 前置条件

- Node.js 18 或更高版本。
- `AMAP_WEBSERVICE_KEY` 存在于环境变量，或被忽略的 `config/project.env`。
- 不创建 `config.json`，不把 Key 放在命令参数、输出或日志。

## 命令

在项目根目录运行：

```bash
node .agents/skills/amap-lbs-skill/scripts/amap.mjs place \
  --keywords "示例博物馆" --region "示例市"

node .agents/skills/amap-lbs-skill/scripts/amap.mjs reverse \
  --location "116.397428,39.90923"

node .agents/skills/amap-lbs-skill/scripts/amap.mjs route \
  --mode drive --origin "116.397428,39.90923" \
  --destination "116.481028,39.989643"

node .agents/skills/amap-lbs-skill/scripts/amap.mjs route \
  --mode transit --origin "116.397428,39.90923" \
  --destination "116.481028,39.989643" --city "010"

node .agents/skills/amap-lbs-skill/scripts/amap.mjs weather --adcode "110101"
```

坐标顺序始终是 `经度,纬度`。路线只记录估算距离和时间，不把估算当成当天实时路况。

## 写入攻略和计划

- 地点候选必须核对名称、地址和行政区，避免同名地点。
- `plan.json` 中保留地址、经纬度、六位天气 adcode、导航链接和数据核验时间。
- 失败或限流时记录 `infocode` 与时间，等待恢复后复核；绝不补造坐标、天气或路线。
- 只有行程中的真实地点才进入 `places`；沿途搜索候选不要全部导入。
