package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const version = "2.0.0"

type CLIOpts struct {
	Pages       int
	Query       string
	Categories  string
	Purity      string
	AtLeast     string
	Resolutions string
	Ratios      string
	Sorting     string
	TopRange    string
	Order       string
	APIKey      string
	Dir         string
	Proxy       string
	Concurrent  int
	Retry       int
	Verbose     bool
	URL         string
}

func parseCLIFlags() *CLIOpts {
	opts := &CLIOpts{}
	flag.IntVar(&opts.Pages, "pages", 0, "下载页数")
	flag.StringVar(&opts.Query, "q", "", "搜索关键词")
	flag.StringVar(&opts.Categories, "categories", "", "分类")
	flag.StringVar(&opts.Purity, "purity", "", "内容过滤")
	flag.StringVar(&opts.AtLeast, "atleast", "", "最低分辨率")
	flag.StringVar(&opts.Resolutions, "resolutions", "", "精确分辨率")
	flag.StringVar(&opts.Ratios, "ratios", "", "宽高比")
	flag.StringVar(&opts.Sorting, "sorting", "", "排序方式")
	flag.StringVar(&opts.TopRange, "topRange", "", "排行范围")
	flag.StringVar(&opts.Order, "order", "", "排序方向")
	flag.StringVar(&opts.APIKey, "apikey", "", "API Key")
	flag.StringVar(&opts.Dir, "dir", "", "下载目录")
	flag.StringVar(&opts.Proxy, "proxy", "", "代理地址")
	flag.IntVar(&opts.Concurrent, "concurrent", 0, "并发下载数")
	flag.IntVar(&opts.Retry, "retry", 0, "失败重试次数")
	flag.BoolVar(&opts.Verbose, "v", false, "显示详细输出")
	flag.Parse()
	remaining := flag.Args()
	if len(remaining) > 0 {
		opts.URL = remaining[0]
	}
	return opts
}

func main() {
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "-version" || arg == "--version" {
			fmt.Printf("wallhaven-dl v%s\n", version)
			return
		}
		if arg == "-h" || arg == "--help" {
			fmt.Println("Wallhaven DL - Wallhaven 壁纸批量下载工具")
			fmt.Println()
			fmt.Println("  直接运行进入 TUI 界面:")
			fmt.Println("    wallhaven-dl")
			fmt.Println()
			fmt.Println("  命令行模式:")
			fmt.Println("    wallhaven-dl <搜索URL> -pages <页数>")
			fmt.Println("    wallhaven-dl <壁纸URL>")
			fmt.Println()
			fmt.Println("  ⚠ PowerShell 中 URL 需用引号包裹:")
			fmt.Println(`    wallhaven-dl "https://wallhaven.cc/search?categories=111&purity=111&..." -pages 5`)
			fmt.Println()
			fmt.Println("  选项:")
			fmt.Println("    -pages <N>     下载页数")
			fmt.Println("    -apikey <KEY>  API Key")
			fmt.Println("    -dir <PATH>    下载目录")
			fmt.Println("    -proxy <URL>   代理地址")
			fmt.Println("    -concurrent <N> 并发数（默认3）")
			fmt.Println("    -retry <N>     重试次数（默认3）")
			fmt.Println("    -v             详细输出")
			fmt.Println("    -version       版本号")
			return
		}
	}

	cfg := LoadConfig()

	if !cfg.Exists() {
		cfg.Save()
	}

	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		opts := parseCLIFlags()
		runCLI(cfg, opts)
	} else if hasCLIFlags() {
		opts := parseCLIFlags()
		runCLI(cfg, opts)
	} else {
		if err := InitLogger(false); err != nil {
			fmt.Printf("日志文件初始化失败: %v\n", err)
		}
		defer CloseLogger()
		if err := runTUI(cfg); err != nil {
			fmt.Printf("TUI 启动失败: %v\n", err)
			os.Exit(1)
		}
	}
}

func hasCLIFlags() bool {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-pages") || strings.HasPrefix(arg, "-apikey") ||
			strings.HasPrefix(arg, "-dir") || strings.HasPrefix(arg, "-proxy") ||
			strings.HasPrefix(arg, "-concurrent") || strings.HasPrefix(arg, "-retry") ||
			strings.HasPrefix(arg, "-q") || strings.HasPrefix(arg, "-categories") ||
			strings.HasPrefix(arg, "-purity") || strings.HasPrefix(arg, "-atleast") ||
			strings.HasPrefix(arg, "-sorting") || strings.HasPrefix(arg, "-topRange") ||
			strings.HasPrefix(arg, "-order") || strings.HasPrefix(arg, "-v") {
			return true
		}
	}
	return false
}
