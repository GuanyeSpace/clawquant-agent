# clawquant-agent

`clawquant-agent` 是一个通过 WebSocket 连接 ClawQuant 平台的独立 Agent 二进制。

当前骨架包含：

- 标准 `cmd/ + internal/ + scripts/` 目录布局
- WebSocket 连接管理器和指令分发器
- 本地 SQLite 初始化
- HMAC 签名与 AES-GCM 解密模块
- `Makefile`
- PowerShell 交叉编译脚本

## 项目结构

```text
.
|-- cmd/
|   `-- agent/
|       `-- main.go
|-- internal/
|   |-- app/
|   |   |-- app.go
|   |   `-- config.go
|   `-- buildinfo/
|       `-- buildinfo.go
|   |-- command/
|   |-- connection/
|   |-- crypto/
|   |-- process/
|   `-- storage/
|-- bin/
|-- scripts/
|   `-- build-cross.ps1
|-- dist/
|-- go.mod
`-- Makefile
```

## 常用命令

```powershell
go test ./...
go run ./cmd/agent --help
powershell -ExecutionPolicy Bypass -File .\scripts\build-cross.ps1
```

如果本机安装了 GNU Make，也可以使用：

```bash
make fmt
make test
make build
make build-linux
make run
make cross-build
```

## 启动

```powershell
.\bin\clawquant-agent.exe --token xxx --secret xxx --server ws://localhost:8080
```

## 版本注入

构建时通过 `ldflags` 注入以下字段：

- `Version`
- `Commit`
- `BuildTime`

默认值分别是 `dev`、`none`、`unknown`。
