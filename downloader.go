package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type DownloadTask struct {
	Wallpaper *Wallpaper
	Detail    *Wallpaper
	SavePath  string
}

type Stats struct {
	Total      int32
	Downloaded int32
	Skipped    int32
	Failed     int32
	TotalBytes int64
}

type WallpaperDownloader interface {
	DownloadAll(tasks []*DownloadTask)
	Cancel()
	IsCancelled() bool
	SetTotal(n int32)
	GetStats() Stats
}

type Downloader struct {
	cfg        *Config
	httpClient *http.Client
	stats      Stats
	cancelled  int32
}

func NewDownloader(httpClient *http.Client, cfg *Config) *Downloader {
	return &Downloader{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func (d *Downloader) Cancel() {
	atomic.StoreInt32(&d.cancelled, 1)
}

func (d *Downloader) IsCancelled() bool {
	return atomic.LoadInt32(&d.cancelled) == 1
}

func (d *Downloader) SetTotal(n int32) {
	atomic.StoreInt32(&d.stats.Total, n)
}

func (d *Downloader) GetStats() Stats {
	return Stats{
		Total:      atomic.LoadInt32(&d.stats.Total),
		Downloaded: atomic.LoadInt32(&d.stats.Downloaded),
		Skipped:    atomic.LoadInt32(&d.stats.Skipped),
		Failed:     atomic.LoadInt32(&d.stats.Failed),
		TotalBytes: atomic.LoadInt64(&d.stats.TotalBytes),
	}
}

func (d *Downloader) DownloadAll(tasks []*DownloadTask) {
	concurrency := d.cfg.Concurrency
	if concurrency < 1 {
		concurrency = 3
	}
	if concurrency > 10 {
		concurrency = 10
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, task := range tasks {
		if d.IsCancelled() {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t *DownloadTask) {
			defer wg.Done()
			defer func() { <-sem }()
			if !d.IsCancelled() {
				d.downloadWithRetry(t)
			} else {
				// 取消时计入 Failed 以保持统计一致
				atomic.AddInt32(&d.stats.Failed, 1)
			}
		}(task)
	}

	wg.Wait()

	// 取消时，未启动的任务也计入 Failed 以保持统计一致
	if d.IsCancelled() {
		done := atomic.LoadInt32(&d.stats.Downloaded) + atomic.LoadInt32(&d.stats.Skipped) + atomic.LoadInt32(&d.stats.Failed)
		remaining := atomic.LoadInt32(&d.stats.Total) - done
		if remaining > 0 {
			atomic.AddInt32(&d.stats.Failed, remaining)
		}
	}
}

func (d *Downloader) downloadWithRetry(task *DownloadTask) {
	imageURL := task.Detail.Path
	if imageURL == "" {
		imageURL = task.Wallpaper.Path
	}

	retries := d.cfg.RetryCount
	if retries < 1 {
		retries = 1
	}

	for attempt := 1; attempt <= retries; attempt++ {
		if d.IsCancelled() {
			atomic.AddInt32(&d.stats.Failed, 1)
			return
		}

		downloaded, written, err := d.downloadFile(imageURL, task.SavePath, task.Detail.FileSize)
		if err == nil {
			if downloaded {
				atomic.AddInt32(&d.stats.Downloaded, 1)
				atomic.AddInt64(&d.stats.TotalBytes, written)
			} else {
				atomic.AddInt32(&d.stats.Skipped, 1)
			}
			d.printProgress(task)
			return
		}

		if attempt < retries {
			logWarn("下载失败 %s（第 %d/%d 次重试）: %v", filepath.Base(task.SavePath), attempt, retries, err)
			time.Sleep(time.Duration(attempt*2) * time.Second)
		} else {
			atomic.AddInt32(&d.stats.Failed, 1)
			logError("下载失败 %s（已重试 %d 次）: %v", filepath.Base(task.SavePath), retries, err)
			d.printProgress(task)
		}
	}
}

func (d *Downloader) printProgress(task *DownloadTask) {
	done := atomic.LoadInt32(&d.stats.Downloaded) + atomic.LoadInt32(&d.stats.Skipped) + atomic.LoadInt32(&d.stats.Failed)
	total := atomic.LoadInt32(&d.stats.Total)

	pct := float64(0)
	if total > 0 {
		pct = float64(done) / float64(total) * 100
	}

	barWidth := 25
	filled := int(float64(barWidth) * pct / 100)
	if filled > barWidth {
		filled = barWidth
	}

	var bar string
	if supportsUnicode() {
		bar = strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	} else {
		bar = strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled)
	}

	name := filepath.Base(task.SavePath)
	if len(name) > 20 {
		name = name[:17] + "..."
	}

	if !IsTUIMode() {
		fmt.Printf("\r  [%s] %5.1f%% %3d/%d  %s  ", bar, pct, done, total, name)
	}

	if done >= total {
		if !IsTUIMode() {
			fmt.Println()
		}
	}
}

func (d *Downloader) downloadFile(downloadURL, savePath string, expectedSize int) (bool, int64, error) {
	if fileExists(savePath) {
		if expectedSize > 0 {
			info, err := os.Stat(savePath)
			if err == nil && info.Size() == int64(expectedSize) {
				logDebug("跳过已存在（校验通过）: %s", savePath)
				return false, 0, nil
			}
			if err == nil && info.Size() != int64(expectedSize) {
				logWarn("文件大小不匹配，重新下载: %s (期望 %d, 实际 %d)", savePath, expectedSize, info.Size())
				os.Remove(savePath)
			}
		} else {
			logDebug("跳过已存在: %s", savePath)
			return false, 0, nil
		}
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return false, 0, fmt.Errorf("构造下载请求失败: %w", err)
	}

	req.Header.Set("User-Agent", "Wallhaven-DL/1.0")

	logDebug("下载: %s -> %s", downloadURL, savePath)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "proxyconnect") {
			return false, 0, fmt.Errorf("代理连接失败，请检查代理设置: %w", err)
		}
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			return false, 0, fmt.Errorf("下载超时，网络可能不稳定: %w", err)
		}
		return false, 0, fmt.Errorf("下载请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return false, 0, fmt.Errorf("访问被拒绝 (403)，可能需要 API Key")
	}
	if resp.StatusCode == 404 {
		return false, 0, fmt.Errorf("文件不存在 (404)，源文件可能已被删除")
	}
	if resp.StatusCode != 200 {
		return false, 0, fmt.Errorf("下载失败 (HTTP %d)", resp.StatusCode)
	}

	dir := filepath.Dir(savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, 0, fmt.Errorf("创建目录失败 [%s]: %w", dir, err)
	}

	tmpPath := savePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return false, 0, fmt.Errorf("创建临时文件失败: %w", err)
	}

	written, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("写入文件失败: %w", err)
	}

	if expectedSize > 0 && written != int64(expectedSize) {
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("文件大小校验失败（期望 %d 字节，实际 %d 字节）", expectedSize, written)
	}

	if err := os.Rename(tmpPath, savePath); err != nil {
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("保存文件失败: %w", err)
	}

	logDebug("下载完成: %s (%d bytes)", savePath, written)
	return true, written, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func BuildDownloadTasks(wallpapers []*Wallpaper, details []*Wallpaper, baseDir string) []*DownloadTask {
	tasks := make([]*DownloadTask, 0, len(wallpapers))

	for i, wp := range wallpapers {
		var detail *Wallpaper
		if i < len(details) && details[i] != nil {
			detail = details[i]
		} else {
			detail = wp
		}

		imageURL := detail.Path
		if imageURL == "" {
			imageURL = wp.Path
		}

		filename := fmt.Sprintf("wallhaven-%s%s", wp.ID, FileExtFromURL(imageURL))
		catDir := CategoryDirName(wp.Category)
		savePath := filepath.Join(baseDir, catDir, filename)

		tasks = append(tasks, &DownloadTask{
			Wallpaper: wp,
			Detail:    detail,
			SavePath:  savePath,
		})
	}

	return tasks
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (d *Downloader) PrintStats() {
	printInfoLn("")
	printInfoLn("  ═══════════════════════════════════════")
	printInfoLn("    下载统计报告")
	printInfoLn("  ═══════════════════════════════════════")
	printInfoLn("    总计:     %d", atomic.LoadInt32(&d.stats.Total))
	printInfoLn("    已下载:   %d (%s)", atomic.LoadInt32(&d.stats.Downloaded), formatBytes(atomic.LoadInt64(&d.stats.TotalBytes)))
	printInfoLn("    已跳过:   %d（文件已存在）", atomic.LoadInt32(&d.stats.Skipped))
	printInfoLn("    失败:     %d", atomic.LoadInt32(&d.stats.Failed))
	printInfoLn("  ═══════════════════════════════════════")
}

func CleanTmpFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			CleanTmpFiles(subDir)
			continue
		}
		if strings.HasSuffix(entry.Name(), ".tmp") {
			tmpPath := filepath.Join(dir, entry.Name())
			os.Remove(tmpPath)
			logDebug("清理临时文件: %s", tmpPath)
		}
	}
}
