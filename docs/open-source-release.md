# 开源发布与隐私检查

本项目允许以 MIT License 使用和再分发代码，但任何公开仓库都必须从经过检查的当前
文件树创建，不能直接公开带真实旅行、成员或部署记录的内部 Git 历史。

## 发布边界

- 只从 `git ls-files` 导出候选文件，不复制原仓库的 `.git`、`.local`、`.deploy`、
  `backups`、`snapshots` 或本机配置。
- 在空目录初始化新的 Git 历史，并使用一次清晰的初始提交。
- 候选仓库先保持 Private；完成全新环境验收和秘密扫描后，再由维护者决定是否改为
  Public。
- 不复用密码、Bootstrap Token、Cookie 或服务器凭据。高德 Key 只能写入候选环境的
  忽略配置，不能进入仓库、Issue、PR、截图或日志。
- 截图只使用虚构旅行、保留测试手机号和脱敏链接，不展示真实地点、成员、订单或费用。

## 候选仓库检查

在候选仓库根目录运行：

```bash
./scripts/check-public-tree.sh
./scripts/test.sh
BOOTSTRAP_TOKEN=static-check docker compose config
./scripts/test-compose-smoke.sh
```

页面改动还要运行：

```bash
cd web
npm run test:e2e
```

再使用当前官方 Gitleaks 扫描当前树和候选仓库的全部历史。扫描输出不得写出秘密正文；
只记录工具版本、候选提交 SHA、发现数量和处理结论。

```bash
gitleaks dir . --redact
gitleaks git . --redact --log-opts="--all"
```

## 全新环境验收

验收者应从候选仓库重新 clone，不共享原工作区的依赖、数据库或运行目录，并按
[安装与部署](getting-started.md) 完成：

1. 复制忽略配置并启动空数据库。
2. 创建首位管理员并登录。
3. 新建一场虚构旅行，确认日期骨架、地点、交通、预订、费用和成员流程。
4. 在桌面与 390px 视口检查登录、创建、只读访问和错误提示。
5. 停止并再次启动容器，确认数据与快照仍在。
6. 运行完整测试与容器烟测，确认 MongoDB 没有宿主机端口。

任何一步需要原仓库的隐藏文件才能完成，都视为发布阻断项，应先修正文档或配置模板。

## 公开后的维护

- 开启 Dependabot、Secret scanning、Push protection 和 Private vulnerability
  reporting。
- 依赖更新必须经过契约、真实 MongoDB、浏览器和容器烟测。
- 公开仓库只保存通用程序与虚构样例；真实攻略继续只存在于使用者自己的 MongoDB 与
  完整备份中。
- 发现秘密泄漏时，先撤销或轮换凭据，再处理 Git 历史；只删除最新文件不能消除泄漏。
