# Wallhaven DL

Go 语言编写的 Wallhaven 壁纸批量下载工具，支持 TUI 交互界面和命令行两种模式。

## 功能特性

- **双模式运行**：无参数启动进入 TUI 界面，带参数启动进入命令行模式
- **批量下载**：支持按搜索结果批量下载壁纸，按类别自动分目录
- **并发控制**：信号量并发下载，默认 3 并发，可自定义
- **速率限制**：自动遵守 Wallhaven API 限制（40 次/分钟），429 自动重试
- **代理支持**：支持配置文件代理或环境变量代理
- **文件校验**：下载后比对文件大小，不匹配自动重新下载
- **优雅中断**：Ctrl+C 优雅退出，统计一致

## 安装

```bash
go build -o wallhaven-dl .
```

Windows 交叉编译：

```bash
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o wallhaven-dl.exe .
```

## 使用方法

### TUI 模式

直接运行进入交互界面：

```bash
wallhaven-dl
```

### 命令行模式

```bash
# 下载搜索结果
wallhaven-dl "https://wallhaven.cc/search?categories=111&purity=111&sorting=toplist&topRange=1M" -pages 5

# 下载单张壁纸
wallhaven-dl "https://wallhaven.cc/w/yqg6r7"

# 使用搜索参数
wallhaven-dl -q "landscape" -categories 111 -sorting toplist -pages 3
```

### 命令行选项

| 选项 | 说明 |
|------|------|
| `-pages <N>` | 下载页数 |
| `-q <关键词>` | 搜索关键词 |
| `-categories <111>` | 分类（General+Anime+People） |
| `-purity <111>` | 内容过滤（SFW+Sketchy+NSFW） |
| `-atleast <分辨率>` | 最低分辨率（如 2560x1440） |
| `-sorting <方式>` | 排序方式（date_added/relevance/random/views/favorites/toplist） |
| `-topRange <范围>` | 排行范围（1d/3d/1w/1M/3M/6M/1y） |
| `-apikey <KEY>` | API Key |
| `-dir <PATH>` | 下载目录 |
| `-proxy <URL>` | 代理地址 |
| `-concurrent <N>` | 并发数（默认 3） |
| `-retry <N>` | 重试次数（默认 3） |
| `-v` | 详细输出 |

## API Key

Wallhaven API Key 用于访问 NSFW 内容和提升速率限制。

获取地址：https://wallhaven.cc/settings/account

如不需 NSFW 内容，可直接跳过。

## 配置文件

首次运行自动在可执行文件同目录生成 `config.json`：

```json
{
  "_comment": "Wallhaven-DL 配置文件 | API Key 获取: https://wallhaven.cc/settings/account",
  "api_key": "",
  "download_dir": "~/Wallhaven",
  "concurrency": 3,
  "retry_count": 3,
  "proxy": ""
}
```

## 下载目录结构

```
Wallhaven/
├── general/    # General 分类
├── anime/      # Anime 分类
└── people/     # People 分类
```

## 运行测试

```bash
go test -v ./...
```

## 技术栈

- Go 1.25+
- [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) - TUI 框架
- [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) - TUI 组件
- [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) - 终端样式

## License

MIT
