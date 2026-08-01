# 第三方说明

项目自身代码使用 MIT License。本文件列出直接依赖和外部工具的独立许可或服务边界；
它不会改变任何第三方组件的许可证。

## 直接运行依赖

- React、React DOM、React Router、TanStack Query、Phosphor Icons、
  `@amap/amap-jsapi-loader` 与 kin-openapi 分别按其发布包中的 MIT License 使用。
- MongoDB Go Driver 按 Apache License 2.0 使用。
- Go `x/crypto` 按其 BSD 风格许可证使用。

依赖源码不复制进本仓库；锁文件固定实际解析版本。自动依赖更新仍须通过契约、真实
MongoDB、浏览器和容器烟测。

## Agent Reach

`.agents/skills/agent-reach` 收录
[Panniantong/Agent-Reach](https://github.com/Panniantong/Agent-Reach) 的完整 Skill 与
平台路由参考，用于网页、代码、社交平台和视频等只读检索。上游以 MIT License 发布；
项目内保留对应 `LICENSE`。

Agent Reach CLI、OpenCLI 浏览器扩展和各平台登录态不随本仓库分发。

## FlyAI CLI

`.agents/skills/flyai` 只记录对外部 npm 包 `@fly-ai/flyai-cli` 的只读调用方式。CLI 本体
不随仓库分发，安装和使用受其当前许可证及飞猪平台条款约束。

## 高德开放平台

`.agents/skills/amap-lbs-skill/scripts/amap.mjs` 是本项目编写的轻量客户端，不包含高德
SDK、Key 或数据集。调用者需要自己的高德开放平台 Key，并遵守高德服务条款、配额和
数据展示要求。
