# clawquant-agent

`clawquant-agent` 的 Go 项目初始化骨架，包含：

- 标准 `cmd/ + internal/ + scripts/` 目录布局
- 可注入版本信息的 CLI 入口
- `Makefile`
- PowerShell 交叉编译脚本

## 项目结构

```text
.
|-- cmd/
|   `-- clawquant-agent/
|       `-- main.go
|-- internal/
|   |-- app/
|   |   |-- runner.go
|   |   `-- runner_test.go
|   `-- buildinfo/
|       `-- buildinfo.go
|-- scripts/
|   `-- build-cross.ps1
|-- dist/
|-- go.mod
`-- Makefile
```

## 常用命令

```powershell
go test ./...
go run ./cmd/clawquant-agent -version
powershell -ExecutionPolicy Bypass -File .\scripts\build-cross.ps1
```

如果本机安装了 GNU Make，也可以使用：

```bash
make fmt
make test
make build
make cross-build
```

## 版本注入

构建时通过 `ldflags` 注入以下字段：

- `Version`
- `Commit`
- `BuildTime`

默认值分别是 `dev`、`none`、`unknown`。
