# BENZHI_README

基于 Go 实现的karst-map-release HTTP API 项目，一款后端服务，已完整实现洞穴测绘成果公开治理 HTTP 服务，覆盖成果包建档、敏感点位不可变登记、四类脱敏变换、确定性检查、逐项复核、整改退回、公开版冻结、不可变凭据签发与验证，并以带前向哈希链的 JSON Lines 事件日志和原子投影快照保证本地恢复完整性。

## 项目说明
- 项目：benzhi-project-9f0ddb23-6767-473b-aaa8-dfc7149162fa
- 项目用途：已完整实现洞穴测绘成果公开治理 HTTP 服务，覆盖成果包建档、敏感点位不可变登记、四类脱敏变换、确定性检查、逐项复核、整改退回、公开版冻结、不可变凭据签发与验证，并以带前向哈希链的 JSON Lines 事件日志和原子投影快照保证本地恢复完整性。
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/karst-map-release -selfcheck -addr=127.0.0.1:19081
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-9f0ddb23-6767-473b-aaa8-dfc7149162fa-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-9f0ddb23-6767-473b-aaa8-dfc7149162fa-arm64 linux/arm64
docker run -it benzhi-project-9f0ddb23-6767-473b-aaa8-dfc7149162fa-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/karst-map-release -selfcheck -addr=127.0.0.1:19081`
