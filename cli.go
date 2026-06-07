package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func runCLI(cfg *Config, opts *CLIOpts) {
	urlArg := opts.URL

	hasSearchParams := opts.Query != "" || opts.Categories != "" || opts.Purity != "" || opts.AtLeast != "" ||
		opts.Resolutions != "" || opts.Ratios != "" || opts.Sorting != "" || opts.TopRange != "" || opts.Order != ""

	if urlArg == "" && !hasSearchParams {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("请粘贴 Wallhaven URL: ")
		urlArg, _ = reader.ReadString('\n')
		urlArg = strings.TrimSpace(urlArg)
	}

	if opts.Pages <= 0 && !isSingleWallpaperURL(urlArg) {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("请输入下载页数: ")
		pagesStr, _ := reader.ReadString('\n')
		pagesStr = strings.TrimSpace(pagesStr)
		val, err := strconv.Atoi(pagesStr)
		if err != nil || val <= 0 {
			fmt.Println("页数必须为正整数")
			os.Exit(1)
		}
		opts.Pages = val
	}

	if opts.APIKey != "" {
		cfg.APIKey = opts.APIKey
	}
	if opts.Dir != "" {
		cfg.DownloadDir = opts.Dir
	}
	if opts.Proxy != "" {
		cfg.Proxy = opts.Proxy
	}
	if opts.Concurrent > 0 {
		cfg.Concurrency = opts.Concurrent
	}
	if opts.Retry > 0 {
		cfg.RetryCount = opts.Retry
	}

	if err := InitLogger(opts.Verbose); err != nil {
		fmt.Printf("日志文件初始化失败: %v\n", err)
	}
	defer CloseLogger()

	CleanTmpFiles(cfg.DownloadDir)

	client := NewClient(cfg.APIKey, cfg.Proxy)
	downloader := NewDownloader(client.HTTPClient(), cfg)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n正在优雅退出...")
		downloader.Cancel()
	}()

	if singleID := extractSingleWallpaperID(urlArg); singleID != "" {
		runCLISingleDownload(client, downloader, cfg, singleID)
		return
	}

	params, err := ParseSearchURL(urlArg)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("开始下载 %d 页壁纸到 %s ...\n", opts.Pages, cfg.DownloadDir)

	startTime := time.Now()

	allWP, allDetails, err := FetchWallpapers(client, params, opts.Pages, downloader.IsCancelled)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
	}

	if len(allWP) == 0 {
		fmt.Println("未找到壁纸。")
		return
	}

	tasks := BuildDownloadTasks(allWP, allDetails, cfg.DownloadDir)
	downloader.SetTotal(int32(len(tasks)))

	fmt.Printf("开始下载 %d 张壁纸...\n", len(tasks))
	downloader.DownloadAll(tasks)

	elapsed := time.Since(startTime)
	stats := downloader.GetStats()
	fmt.Printf("\n下载完成: 已下载=%d 跳过=%d 失败=%d 耗时=%s\n",
		stats.Downloaded,
		stats.Skipped,
		stats.Failed,
		elapsed.Round(time.Second))
}

func runCLISingleDownload(client *Client, downloader *Downloader, cfg *Config, id string) {
	detail, err := client.GetWallpaperDetail(id)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		os.Exit(1)
	}

	tasks := BuildDownloadTasks([]*Wallpaper{detail}, []*Wallpaper{detail}, cfg.DownloadDir)
	downloader.SetTotal(1)

	fmt.Printf("下载壁纸 %s (%s, %s)...\n", detail.ID, detail.Resolution, detail.Category)
	downloader.DownloadAll(tasks)

	stats := downloader.GetStats()
	if stats.Downloaded > 0 {
		fmt.Println("下载完成:", tasks[0].SavePath)
	} else if stats.Skipped > 0 {
		fmt.Println("文件已存在:", tasks[0].SavePath)
	}
}
