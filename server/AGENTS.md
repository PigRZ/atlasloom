# Go 服务目录

这里包含 Go API、静态站点服务以及所有服务端测试。

- 主模块固定为 `my-travel/server`，项目内部 import 使用该前缀。不得绑定代码托管
  平台的用户或组织，不得增加第二模块路径、`replace` 或运行时仓库识别。
- `cmd/` 只做进程装配；业务规则必须留在 `internal/`。
- 依赖方向保持为 HTTP/命令入口 → service → repository/model/snapshot，不能反向依赖。
- MongoDB 是唯一运行时数据源，业务写入必须和完整快照处于同一事务边界。
- `platform-type` 是标签当前分类之一；预订与资料只能引用该分类，资料记录人必须是
  当前 Trip 已选择的基础成员。
- 管理员跨 Trip 只可导出完整备份或逻辑删除，不能据此读取、修改资源或操作快照。
- `repository.Initialize` 只接受当前模型：先只读校验全部集合，再安装严格 validator、
  完整受管索引和 `system_metadata/current-model` 契约指纹。任何不匹配都应阻止启动，
  不得静默修数据、放宽校验或保留未受管索引。
- 文件镜像失败不回滚已提交的数据库快照；Trip 与基础库快照保持 `pending`，由启动
  扫描和周期补偿继续重试，直到明确标记为 `mirrored`。
- HTTP、鉴权或模型变化必须同步修改 `api/paths/`、`api/components/`、前端类型和
  契约测试；不要把具体接口重新写回 `api/openapi.yaml`。
- 新依赖需更新 `go.mod`/`go.sum`，不提交编译产物。
- 修改后运行 `go test ./...`、`go test -race ./...`、`go vet ./...`。数据库契约或事务
  变化还要运行根目录 `scripts/test-mongo-integration.sh`；覆盖率不得低于脚本门槛。
- 数据结构变化必须用当前完整 Trip 与基础库快照在一次性副本集做语义往返，不能操作
  服务器来验证实现。
- 完成前按根规则评估本文件和根 `AGENTS.md`。
