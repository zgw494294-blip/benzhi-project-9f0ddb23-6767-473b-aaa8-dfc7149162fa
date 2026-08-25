# karst-map-release

`karst-map-release` 是面向洞穴测绘机构的成果公开治理服务。它通过 JSON HTTP API 将测绘成果包依次推进到敏感点位登记、脱敏修订、确定性检查、专业复核、公开版冻结和发布凭据签发，避免洞穴入口、珍稀生境及未保护遗址的原始坐标被意外公开。

服务采用本地事件日志持久化。`events.jsonl` 中的事件带递增序号和前向校验链；`projection.json` 是经文件同步和原子替换生成的查询投影。启动时会验证完整事件链，快照缺失或不一致时自动重放恢复。所有写 API 都要求 `Idempotency-Key`，并通过请求体中的 `expectedVersion` 防止陈旧更新。

## 构建与运行

要求 Go 1.22 或更高版本。

```bash
go build ./cmd/karst-map-release
go run ./cmd/karst-map-release -addr=127.0.0.1:19081 -data-dir=./data/karst-map-release
```

默认监听 `127.0.0.1:19081`，不会绑定公开网卡。可用 `-addr=127.0.0.1:<port>` 覆盖，也可设置 `PORT` 为端口号，此时监听 `127.0.0.1:<PORT>`。为避免误公开，显式地址也必须是回环 IP。

健康检查为 `GET /healthz`。核心公开入口如下：

- `POST /v1/survey-packages` 建档，`PATCH /v1/survey-packages/{packageId}/metadata` 在草拟或退回状态修订元数据；
- `POST /v1/survey-packages/{packageId}/sensitive-sites` 登记点位，`POST` 与 `GET /v1/survey-packages/{packageId}/sensitive-sites/{siteId}/revisions` 分别追加不可变更正和查询修订时间线；
- `POST /v1/survey-packages/{packageId}/redaction-revisions/preview` 无副作用预演，`POST /v1/survey-packages/{packageId}/redaction-revisions` 正式提交候选版并自动执行深度一致性检查；
- `POST /v1/survey-packages/{packageId}/review/decisions` 分批保存裁决，`POST /v1/survey-packages/{packageId}/review` 完成通过或退回；
- `POST /v1/survey-packages/{packageId}/freeze` 原子固化公开清单并签发凭据，`GET /v1/survey-packages/{packageId}/credential` 查询凭据与清单，`POST /v1/release-credentials/verify` 执行诊断式核验。

除只读预演和查询外，写 API 均要求 `Idempotency-Key` 和当前 `expectedVersion`。预演、成果包、点位时间线、整改摘要、清单与凭据核验响应都不会包含 `originalCoordinate`；发布清单只返回图层计数、图层摘要、变换汇总和裁决汇总，不返回公开要素内容或坐标。

## 测试与自检

```bash
go test ./...
go run ./cmd/karst-map-release -selfcheck -addr=127.0.0.1:19081
```

`-selfcheck` 会启动真实回环 HTTP 监听，在临时持久化目录中完整执行建档、点位登记、脱敏、检查、复核、冻结、发证和验证，然后在有界超时内关闭并自行退出。
