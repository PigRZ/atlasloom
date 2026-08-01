# 贡献指南

感谢参与 Atlasloom。项目优先保证真实旅行数据安全、当前模型单一和本地可验证。

## 开始

```bash
cp config/project.env.example config/project.env
chmod 600 config/project.env
./scripts/start.sh
```

开发测试需要 Go 1.26、Node.js 24+、Docker Compose 与 Buildx。首次使用见
[安装与部署](docs/getting-started.md)。

## 改动前

- 领域或接口：读 `docs/data-model.md`、`docs/architecture.md`。
- 前端交互：读 `web/AGENTS.md`。
- 后端或数据库：读 `server/AGENTS.md`。
- 发布、同步、恢复：读 `scripts/AGENTS.md`、`docs/data-workflow.md`。

通用代码不得写死某次旅行的城市、路线、成员、标签或攻略。

## 数据和秘密

不要提交、粘贴或截图：

- `config/project.env`、`config/users.json`
- `.local/`、`.deploy/`、快照、备份、数据库文件
- 真实姓名、手机号、订单、费用、住宿和路线
- 密码、Key、Token、Cookie、二维码和证书私钥

测试只使用明显虚构的姓名和保留号码。提交前运行：

```bash
./scripts/check-public-tree.sh
```

发现泄漏时停止 push，先轮换凭据并联系维护者处理历史；只删除工作树文件不够。

## 实现规则

- 用户可见页面、表单、错误和脚本使用中文。
- 日期为 `YYYY-MM-DD`；时间戳为带时区 ISO-8601；关联使用不可变 ID。
- 所有业务写入鉴权、校验并发基准，并在事务中产生所属聚合完整快照。
- 资源字段变化同时更新模型、validator、仓库、服务、OpenAPI、前端类型/表单、测试和
  `docs/data-model.md`。
- 不增加双模型解析、双写或绕过 API 的数据入口。
- 远程服务器不是调试环境；代码和数据都先在本地副本验证。

## 验证

```bash
./scripts/test.sh
BOOTSTRAP_TOKEN=static-check docker compose config
```

页面改动还要：

```bash
cd web
npm run test:e2e
```

验证桌面和 390px 视口、刷新/返回路径、键盘焦点、控制台错误和横向溢出。数据变化还需
当前完整快照往返和隔离 MongoDB 演练。

Compose、镜像、网关或响应头改动还要运行 `./scripts/test-compose-smoke.sh`；它使用
隔离数据库和临时端口，不应依赖现有 `.local` 数据。

## Pull Request

PR 请说明：

- 用户问题和不变量。
- 变更范围及未改内容。
- 运行过的验证与结果。
- 数据/隐私、兼容性和回滚风险。
- 页面改动的桌面与 390px 证据，或说明为何不适用。

不要在 PR 描述或日志中放真实配置与快照。
