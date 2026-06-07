# AGENTS.md — Wallhaven DL 项目交接文档

## 项目概览

Wallhaven DL 是一个 Go 语言编写的 Wallhaven 壁纸批量下载工具，支持 TUI 交互界面和命令行两种模式。编译为单个可执行文件，零运行时依赖。

- **版本**: v2.0.0
- **语言**: Go 1.25+
- **目标平台**: Windows amd64（也支持 Linux/macOS）
- **外部依赖**: charmbracelet/bubbletea + bubbles + lipgloss（TUI 框架）

## 编译命令

```bash
# 开发编译
go build -o wallhaven-dl .

# Windows 发布版（带版本号）
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o wallhaven-dl.exe .

# 代码检查
go vet ./...
```

## 文件结构

```
Wallhaven-go/
├── main.go            # 入口：TUI 优先，有 CLI 参数时降级为命令行模式
├── tui.go             # TUI 界面：bubbletea 模型，4 个 phase 状态机
├── cli.go             # 命令行模式：flag 参数解析 + 交互式输入降级
├── wallhaven.go       # API 客户端：搜索、详情、速率限制、代理、错误处理
├── wallhaven_test.go  # 单元测试与集成测试
├── downloader.go      # 并发下载器：信号量并发、重试、校验、进度条、统计
├── config.go          # 配置管理：JSON 持久化到可执行文件同目录
├── logger.go          # 日志系统：debug 级写文件，verbose 模式同步到控制台
├── utils.go           # 共享工具：URL 解析、标签映射、Unicode 检测
├── go.mod             # 依赖声明
├── go.sum             # 依赖校验
├── .gitignore         # Git 忽略规则
├── README.md          # 项目说明文档
└── AGENTS.md          # 项目交接文档
```

## 架构设计

### 双模式入口

```
main.go
  ├─ 无参数 → TUI 模式 (tui.go)
  └─ 有参数/URL → CLI 模式 (cli.go)
```

### TUI 状态机

```
phaseInput → phaseFetching → phaseDownloading → phaseDone
  (输入)       (API获取)        (并发下载)        (统计报告)
```

- `phaseInput`: 4 个 textinput 字段（URL/页数/APIKey/代理）+ 开始按钮
- `phaseFetching`: spinner 动画，后台 goroutine 调用 API
- `phaseDownloading`: 渐变进度条 + 实时统计，300ms 轮询更新
- `phaseDone`: 统计报告，按 `r` 重来，按 `q` 退出

### 数据流

```
用户输入 URL
  → ParseSearchURL() 解析为 SearchParams
  → Client.Search() 逐页搜索（搜索结果已含 /full/ 路径，无需逐个请求详情）
  → BuildDownloadTasks() 构建 DownloadTask 列表（按类别分子目录）
  → Downloader.DownloadAll() 信号量并发下载（默认3并发）
```

### 关键设计决策

1. **搜索 API 已返回完整数据**：`path` 字段已包含 `/full/` 原图路径，`file_size`/`resolution`/`category` 均已包含，无需逐个请求 `/w/{id}` 详情接口。仅单张壁纸下载时才调用详情接口。

2. **API 速率限制**：客户端自行维护请求时间戳数组，限制 40 次/分钟（API 限制 45 次/分钟，留余量）。429 错误自动等待 60 秒重试，最多 3 次。

3. **代理支持**：优先级：配置文件 `proxy` 字段 > `HTTP_PROXY`/`HTTPS_PROXY` 环境变量。API 客户端和下载器各自创建独立 HTTP Transport。

4. **文件校验**：下载完成后比对 `file_size`；已存在文件若大小不匹配会自动重新下载。

5. **配置文件位置**：存放在可执行文件同目录 `config.json`，非用户主目录。`PerPage` 字段 API 返回字符串，用 `json:"per_page,string"` tag 处理。

## Wallhaven API v1 要点

| 端点 | 用途 |
|------|------|
| `GET /api/v1/search` | 搜索壁纸，返回 24 张/页，含完整 path/file_size |
| `GET /api/v1/w/{id}` | 单张壁纸详情（仅单张下载时使用） |
| `GET /api/v1/settings` | 用户设置（未使用） |

**速率限制**: 45 次/分钟，超出返回 429。NSFW 内容需 API Key。

**搜索参数映射**:
- URL `categories=111` → API `categories=111`（General+Anime+People）
- URL `purity=111` → API `purity=111`（SFW+Sketchy+NSFW）
- URL `atleast=2560x1440` → API `atleast=2560x1440`
- URL `sorting=toplist` → API `sorting=toplist`
- URL `topRange=1M` → API `topRange=1M`

**响应结构**:
```json
{
  "data": [{ "id": "yqg6r7", "path": "https://w.wallhaven.cc/full/yq/wallhaven-yqg6r7.jpg", "file_size": 2937012, "category": "general", ... }],
  "meta": { "current_page": 1, "last_page": 134, "per_page": "24", "total": 3214 }
}
```

注意 `per_page` 是字符串类型，已用 `json:"per_page,string"` 处理。

## 已知问题与改进方向

1. **TUI 下载进度轮询**：当前用 300ms `tea.Tick` 轮询 `atomic` 统计值，可改为 channel 推送（低优先级，当前方案够用）
2. ~~**CLI 模式 flag 解析**~~：已修复 — flag 定义和解析统一在 `main.go` 的 `parseCLIFlags()` 中，`cli.go` 接收 `CLIOpts` 结构体
3. ~~**Windows 终端兼容**~~：已修复 — `supportsUnicode()` 检测终端能力，非 Unicode 终端自动使用 `#-` 替代 `█░`
4. ~~**日志文件与 TUI 冲突**~~：已修复 — `SetTUIMode(true)` 在 TUI 启动前调用，`printInfoLn`/`printProgress` 在 TUI 模式下仅写日志文件
5. ~~**搜索结果未校验 path 格式**~~：已修复 — `isFullPath()` 检测 `/full/` 路径，非 `/full/` 时自动回退到详情接口

## 运行时文件

| 文件 | 位置 | 说明 |
|------|------|------|
| `config.json` | 可执行文件同目录 | 配置文件，首次运行自动生成 |
| `wallhaven-dl.log` | 可执行文件同目录 | Debug 日志，>2MB 自动轮转 |
| `wallhaven-dl.log.old` | 可执行文件同目录 | 日志备份 |
| `Wallhaven/` | 用户主目录 | 默认下载目录 |
| `Wallhaven/anime/` | 下载目录子目录 | Anime 分类 |
| `Wallhaven/general/` | 下载目录子目录 | General 分类 |
| `Wallhaven/people/` | 下载目录子目录 | People 分类 |
