package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================
// Bug #1: RetryCount=0 导致 downloadWithRetry 循环不执行
// ============================================================

func TestRetryCountZeroStillDownloads(t *testing.T) {
	// 当 RetryCount=0 时，downloadWithRetry 的 for 循环
	// for attempt := 1; attempt <= d.cfg.RetryCount 永远不执行
	// 导致文件完全不会被下载
	tmpDir := t.TempDir()

	// 创建一个简单的 HTTP 服务器返回测试图片
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-image-data"))
	}))
	defer server.Close()

	cfg := &Config{
		DownloadDir: tmpDir,
		Concurrency: 1,
		RetryCount:  0, // BUG: 这会导致循环不执行
	}

	downloader := NewDownloader(server.Client(), cfg)

	wp := &Wallpaper{
		ID:       "test01",
		Path:     server.URL + "/image.jpg",
		Category: "general",
		FileSize: 0, // 不校验大小
	}
	detail := &Wallpaper{
		ID:       "test01",
		Path:     server.URL + "/image.jpg",
		Category: "general",
		FileSize: 0,
	}

	tasks := []*DownloadTask{
		{Wallpaper: wp, Detail: detail, SavePath: filepath.Join(tmpDir, "general", "wallhaven-test01.jpg")},
	}

	downloader.SetTotal(1)
	downloader.DownloadAll(tasks)

	stats := downloader.GetStats()
	// 期望：即使 RetryCount=0，也应该成功下载
	if stats.Downloaded == 0 && stats.Skipped == 0 {
		t.Errorf("RetryCount=0 时文件未被下载: Downloaded=%d, Skipped=%d, Failed=%d",
			stats.Downloaded, stats.Skipped, stats.Failed)
	}
}

// ============================================================
// Bug #2: extractSingleWallpaperID 不处理 URL 查询参数
// ============================================================

func TestExtractSingleWallpaperIDWithQueryParams(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://wallhaven.cc/w/abc123", "abc123"},
		{"https://wallhaven.cc/w/abc123?ref=xxx", "abc123"},
		{"https://wallhaven.cc/w/yqg6r7/", "yqg6r7"},
		{"https://wallhaven.cc/w/yqg6r7?foo=bar&baz=1", "yqg6r7"},
		{"", ""},
		{"https://wallhaven.cc/search?categories=111", ""},
	}

	for _, tt := range tests {
		got := extractSingleWallpaperID(tt.url)
		if got != tt.want {
			t.Errorf("extractSingleWallpaperID(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// ============================================================
// Bug #3: LoadConfig 静默忽略无效 JSON
// ============================================================

func TestLoadConfigInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// 写入无效 JSON
	os.WriteFile(configPath, []byte("{invalid json!!!"), 0644)

	// 临时修改 executableDir 使 config 读取我们的临时目录
	origDir := executableDir
	executableDir = tmpDir
	defer func() { executableDir = origDir }()

	cfg := LoadConfig()

	// 即使 JSON 无效，也应返回合理的默认值
	if cfg.Concurrency != 3 {
		t.Errorf("无效 JSON 时 Concurrency 应为默认值 3，实际为 %d", cfg.Concurrency)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("无效 JSON 时 RetryCount 应为默认值 3，实际为 %d", cfg.RetryCount)
	}
}

// ============================================================
// Bug #4: hasCLIFlags 遗漏 -v 标志
// ============================================================

func TestHasCLIFlagsVerbose(t *testing.T) {
	// 保存原始 os.Args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"wallhaven-dl", "-v"}, true},
		{[]string{"wallhaven-dl", "-pages", "5"}, true},
		{[]string{"wallhaven-dl", "-apikey", "xxx"}, true},
		{[]string{"wallhaven-dl"}, false},
	}

	for _, tt := range tests {
		os.Args = tt.args
		got := hasCLIFlags()
		if got != tt.want {
			t.Errorf("hasCLIFlags() with args=%v = %v, want %v", tt.args, got, tt.want)
		}
	}
}

// ============================================================
// Bug #5: ParseSearchURL 应使用类型化错误
// ============================================================

func TestParseSearchURLSingleWallpaperError(t *testing.T) {
	_, err := ParseSearchURL("https://wallhaven.cc/w/abc123")

	if err == nil {
		t.Fatal("期望返回错误，但返回了 nil")
	}

	// 应该是 SingleWallpaperError 类型，而非通用字符串错误
	if !isSingleWallpaperError(err) {
		t.Errorf("ParseSearchURL 对单张壁纸 URL 应返回 SingleWallpaperError，实际: %T: %v", err, err)
	}
}

// ============================================================
// Bug #7: cli.go 直接访问 downloader.stats 绕过原子操作
// ============================================================

func TestDownloaderSetTotalUsesAtomic(t *testing.T) {
	cfg := &Config{Concurrency: 1, RetryCount: 1}
	downloader := NewDownloader(http.DefaultClient, cfg)

	downloader.SetTotal(42)
	stats := downloader.GetStats()

	if stats.Total != 42 {
		t.Errorf("SetTotal(42) 后 GetStats().Total = %d, want 42", stats.Total)
	}
}

// ============================================================
// Bug #8: 取消下载后统计不一致
// ============================================================

func TestCancelledDownloadStatsConsistent(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("data"))
	}))
	defer server.Close()

	cfg := &Config{
		DownloadDir: tmpDir,
		Concurrency: 1,
		RetryCount:  1,
	}

	downloader := NewDownloader(server.Client(), cfg)

	var tasks []*DownloadTask
	for i := 0; i < 5; i++ {
		wp := &Wallpaper{
			ID:       fmt.Sprintf("test%02d", i),
			Path:     server.URL + "/image.jpg",
			Category: "general",
			FileSize: 0,
		}
		tasks = append(tasks, &DownloadTask{
			Wallpaper: wp,
			Detail:    wp,
			SavePath:  filepath.Join(tmpDir, "general", fmt.Sprintf("wallhaven-test%02d.jpg", i)),
		})
	}

	downloader.SetTotal(int32(len(tasks)))

	// 启动下载
	done := make(chan struct{})
	go func() {
		downloader.DownloadAll(tasks)
		close(done)
	}()

	// 立即取消
	downloader.Cancel()

	<-done

	stats := downloader.GetStats()
	processed := stats.Downloaded + stats.Skipped + stats.Failed
	// 关键断言：已处理数不应超过总数
	if processed > stats.Total {
		t.Errorf("已处理数 (%d) 超过总数 (%d)", processed, stats.Total)
	}
	// 取消后统计应一致：Downloaded + Skipped + Failed == Total
	if processed != stats.Total {
		t.Errorf("取消后统计不一致: Total=%d, Downloaded=%d, Skipped=%d, Failed=%d (已处理=%d)",
			stats.Total, stats.Downloaded, stats.Skipped, stats.Failed, processed)
	}
}

// ============================================================
// 辅助：SearchResult JSON 解析测试（回归保护）
// ============================================================

func TestSearchResultJSONParsing(t *testing.T) {
	raw := `{
		"data": [
			{
				"id": "yqg6r7",
				"path": "https://w.wallhaven.cc/full/yq/wallhaven-yqg6r7.jpg",
				"file_size": 2937012,
				"category": "general"
			}
		],
		"meta": {
			"current_page": 1,
			"last_page": 134,
			"per_page": "24",
			"total": 3214
		}
	}`

	var result SearchResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("解析搜索结果失败: %v", err)
	}

	if result.Meta.PerPage != 24 {
		t.Errorf("PerPage = %d, want 24 (从字符串 \"24\" 解析)", result.Meta.PerPage)
	}
	if len(result.Data) != 1 {
		t.Fatalf("Data 长度 = %d, want 1", len(result.Data))
	}
	if result.Data[0].ID != "yqg6r7" {
		t.Errorf("ID = %q, want %q", result.Data[0].ID, "yqg6r7")
	}
	if !isFullPath(result.Data[0].Path) {
		t.Errorf("isFullPath(%q) = false, want true", result.Data[0].Path)
	}
}

// ============================================================
// FetchWallpapers 集成测试
// ============================================================

func TestFetchWallpapersWithMockAPI(t *testing.T) {
	searchResp := SearchResult{
		Data: []Wallpaper{
			{ID: "abc001", Path: "https://w.wallhaven.cc/full/ab/wallhaven-abc001.jpg", Category: "anime", FileSize: 1000},
			{ID: "abc002", Path: "https://w.wallhaven.cc/full/ab/wallhaven-abc002.png", Category: "general", FileSize: 2000},
		},
		Meta: SearchMeta{CurrentPage: 1, LastPage: 1, PerPage: 24, Total: 2},
	}

	mock := &mockAPI{
		searchResult: &searchResp,
	}

	params := &SearchParams{Categories: "111", Sorting: "toplist"}
	allWP, allDetails, err := FetchWallpapers(mock, params, 1, nil)

	if err != nil {
		t.Fatalf("FetchWallpapers 失败: %v", err)
	}
	if len(allWP) != 2 {
		t.Errorf("allWP 长度 = %d, want 2", len(allWP))
	}
	if len(allDetails) != 2 {
		t.Errorf("allDetails 长度 = %d, want 2", len(allDetails))
	}
	// 搜索结果已含 /full/ 路径，不应调用详情 API
	if mock.detailCallCount != 0 {
		t.Errorf("详情 API 被调用了 %d 次，期望 0 次（搜索结果已含完整路径）", mock.detailCallCount)
	}
}

func TestFetchWallpapersFallbackToDetail(t *testing.T) {
	searchResp := SearchResult{
		Data: []Wallpaper{
			{ID: "abc001", Path: "https://th.wallhaven.cc/small/ab/abc001.jpg", Category: "anime", FileSize: 0},
		},
		Meta: SearchMeta{CurrentPage: 1, LastPage: 1, PerPage: 24, Total: 1},
	}

	detailWP := &Wallpaper{
		ID: "abc001", Path: "https://w.wallhaven.cc/full/ab/wallhaven-abc001.jpg", Category: "anime", FileSize: 5000,
	}

	mock := &mockAPI{
		searchResult:  &searchResp,
		detailResult:  detailWP,
		detailCallCount: 0,
	}

	params := &SearchParams{Categories: "111"}
	allWP, allDetails, err := FetchWallpapers(mock, params, 1, nil)

	if err != nil {
		t.Fatalf("FetchWallpapers 失败: %v", err)
	}
	if len(allWP) != 1 {
		t.Fatalf("allWP 长度 = %d, want 1", len(allWP))
	}
	// 非 /full/ 路径应触发详情 API 调用
	if mock.detailCallCount != 1 {
		t.Errorf("详情 API 调用次数 = %d, want 1", mock.detailCallCount)
	}
	// allDetails 应使用详情 API 返回的完整路径
	if allDetails[0].Path != detailWP.Path {
		t.Errorf("allDetails[0].Path = %q, want %q", allDetails[0].Path, detailWP.Path)
	}
}

// ============================================================
// mock API
// ============================================================

type mockAPI struct {
	searchResult   *SearchResult
	detailResult   *Wallpaper
	searchErr      error
	detailErr      error
	detailCallCount int
}

func (m *mockAPI) Search(params *SearchParams, page int) (*SearchResult, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.searchResult, nil
}

func (m *mockAPI) GetWallpaperDetail(id string) (*Wallpaper, error) {
	m.detailCallCount++
	if m.detailErr != nil {
		return nil, m.detailErr
	}
	return m.detailResult, nil
}

// ============================================================
// isSingleWallpaperError 辅助（Bug #5 修复前会编译失败）
// ============================================================

func isSingleWallpaperError(err error) bool {
	_, ok := err.(*SingleWallpaperError)
	return ok
}

// ============================================================
// BuildDownloadTasks 测试
// ============================================================

func TestBuildDownloadTasks(t *testing.T) {
	wallpapers := []*Wallpaper{
		{ID: "abc001", Category: "anime"},
		{ID: "abc002", Category: "general"},
		{ID: "abc003", Category: "people"},
	}
	details := []*Wallpaper{
		{ID: "abc001", Path: "https://w.wallhaven.cc/full/ab/wallhaven-abc001.jpg", Category: "anime"},
		{ID: "abc002", Path: "https://w.wallhaven.cc/full/ab/wallhaven-abc002.png", Category: "general"},
		{ID: "abc003", Path: "https://w.wallhaven.cc/full/ab/wallhaven-abc003.jpg", Category: "people"},
	}

	tasks := BuildDownloadTasks(wallpapers, details, "/tmp/Wallhaven")

	if len(tasks) != 3 {
		t.Fatalf("任务数 = %d, want 3", len(tasks))
	}

	expectedDirs := []string{"anime", "general", "people"}
	for i, task := range tasks {
		expectedDir := filepath.Join("/tmp/Wallhaven", expectedDirs[i])
		if !strings.HasPrefix(task.SavePath, expectedDir) {
			t.Errorf("task[%d].SavePath = %q, 期望在 %s 下", i, task.SavePath, expectedDir)
		}
	}
}

// ============================================================
// Config 测试
// ============================================================

func TestConfigSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	origDir := executableDir
	executableDir = tmpDir
	defer func() { executableDir = origDir }()

	cfg := &Config{
		ExeDir:      tmpDir,
		APIKey:      "test-key",
		DownloadDir: "/tmp/test",
		Concurrency: 5,
		RetryCount:  2,
		Proxy:       "http://proxy:8080",
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	loaded := LoadConfig()
	if loaded.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", loaded.APIKey, "test-key")
	}
	if loaded.Concurrency != 5 {
		t.Errorf("Concurrency = %d, want 5", loaded.Concurrency)
	}
	if loaded.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", loaded.RetryCount)
	}
	if loaded.Proxy != "http://proxy:8080" {
		t.Errorf("Proxy = %q, want %q", loaded.Proxy, "http://proxy:8080")
	}
}

func TestConfigDefaultValues(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Concurrency != 3 {
		t.Errorf("默认 Concurrency = %d, want 3", cfg.Concurrency)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("默认 RetryCount = %d, want 3", cfg.RetryCount)
	}
}

// ============================================================
// FileExtFromURL 测试
// ============================================================

func TestFileExtFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://w.wallhaven.cc/full/ab/wallhaven-abc001.jpg", ".jpg"},
		{"https://w.wallhaven.cc/full/ab/wallhaven-abc002.png", ".png"},
		{"https://example.com/image.JPEG", ".jpeg"},
		{"https://example.com/noext", ".jpg"},
		{"", ".jpg"},
	}

	for _, tt := range tests {
		got := FileExtFromURL(tt.url)
		if got != tt.want {
			t.Errorf("FileExtFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// ============================================================
// CategoryDirName 测试
// ============================================================

func TestCategoryDirName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"anime", "anime"},
		{"general", "general"},
		{"people", "people"},
		{"Anime", "anime"},
		{"unknown", "other"},
		{"", "other"},
	}

	for _, tt := range tests {
		got := CategoryDirName(tt.input)
		if got != tt.want {
			t.Errorf("CategoryDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ============================================================
// formatBytes 测试
// ============================================================

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.bytes)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

// ============================================================
// Downloader 并发安全测试
// ============================================================

func TestDownloaderConcurrentSafety(t *testing.T) {
	tmpDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("data"))
	}))
	defer server.Close()

	cfg := &Config{
		DownloadDir: tmpDir,
		Concurrency: 3,
		RetryCount:  1,
	}

	downloader := NewDownloader(server.Client(), cfg)

	var tasks []*DownloadTask
	for i := 0; i < 10; i++ {
		wp := &Wallpaper{
			ID:       fmt.Sprintf("safe%02d", i),
			Path:     fmt.Sprintf("%s/img%d.jpg", server.URL, i),
			Category: "general",
			FileSize: 0,
		}
		tasks = append(tasks, &DownloadTask{
			Wallpaper: wp,
			Detail:    wp,
			SavePath:  filepath.Join(tmpDir, "general", fmt.Sprintf("wallhaven-safe%02d.jpg", i)),
		})
	}

	downloader.SetTotal(int32(len(tasks)))
	downloader.DownloadAll(tasks)

	stats := downloader.GetStats()
	processed := stats.Downloaded + stats.Skipped + stats.Failed
	if processed != stats.Total {
		t.Errorf("并发下载后统计不一致: Total=%d, 已处理=%d (D=%d,S=%d,F=%d)",
			stats.Total, processed, stats.Downloaded, stats.Skipped, stats.Failed)
	}
}

// ============================================================
// Stats 原子操作测试
// ============================================================

func TestStatsAtomicAccess(t *testing.T) {
	cfg := &Config{Concurrency: 1, RetryCount: 1}
	d := NewDownloader(http.DefaultClient, cfg)

	d.SetTotal(100)
	stats := d.GetStats()
	if stats.Total != 100 {
		t.Errorf("SetTotal(100) 后 GetStats().Total = %d", stats.Total)
	}

	// 模拟并发更新
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt32(&d.stats.Downloaded, 1)
		}()
	}
	wg.Wait()

	stats = d.GetStats()
	if stats.Downloaded != 100 {
		t.Errorf("100 次并发 AddInt32 后 Downloaded = %d, want 100", stats.Downloaded)
	}
}

// ============================================================
// Round 2: 更深层的 bug 测试
// ============================================================

// Bug #9: SetTotal 非原子写入 — 与 DownloadAll 中 atomic.LoadInt32 竞争
func TestSetTotalAtomicConsistency(t *testing.T) {
	cfg := &Config{Concurrency: 1, RetryCount: 1}
	d := NewDownloader(http.DefaultClient, cfg)

	// 并发 SetTotal 和 GetStats 不应导致 race
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.SetTotal(50)
		}()
		go func() {
			defer wg.Done()
			_ = d.GetStats()
		}()
	}
	wg.Wait()
}

// Bug #10: downloadWithRetry 取消时 return 不计 Failed
func TestDownloadWithRetryCancelledMidRetryCountsFailed(t *testing.T) {
	tmpDir := t.TempDir()

	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// 第一个请求永远不返回成功，让重试循环持续
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer server.Close()

	cfg := &Config{
		DownloadDir: tmpDir,
		Concurrency: 1,
		RetryCount:  100, // 大量重试
	}

	downloader := NewDownloader(server.Client(), cfg)

	wp := &Wallpaper{
		ID:       "cancel01",
		Path:     server.URL + "/fail.jpg",
		Category: "general",
		FileSize: 0,
	}

	tasks := []*DownloadTask{
		{Wallpaper: wp, Detail: wp, SavePath: filepath.Join(tmpDir, "general", "wallhaven-cancel01.jpg")},
	}

	downloader.SetTotal(1)

	// 在另一个 goroutine 中启动下载
	done := make(chan struct{})
	go func() {
		downloader.DownloadAll(tasks)
		close(done)
	}()

	// 等待至少一次请求发生
	for atomic.LoadInt32(&callCount) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	// 取消下载
	downloader.Cancel()
	<-done

	stats := downloader.GetStats()
	processed := stats.Downloaded + stats.Skipped + stats.Failed
	// 关键断言：取消后统计应一致
	if processed != stats.Total {
		t.Errorf("取消后统计不一致: Total=%d, Downloaded=%d, Skipped=%d, Failed=%d (已处理=%d)",
			stats.Total, stats.Downloaded, stats.Skipped, stats.Failed, processed)
	}
}

// Bug #11: GetStats 并发读取一致性
func TestGetStatsConcurrentRead(t *testing.T) {
	cfg := &Config{Concurrency: 1, RetryCount: 1}
	d := NewDownloader(http.DefaultClient, cfg)
	d.SetTotal(1000)

	stop := make(chan struct{})

	// 一个 goroutine 持续更新 stats
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				atomic.AddInt32(&d.stats.Downloaded, 1)
				atomic.AddInt64(&d.stats.TotalBytes, 100)
			}
		}
	}()

	// 另一个 goroutine 持续读取 stats
	var readerWg sync.WaitGroup
	readerWg.Add(1)
	go func() {
		defer readerWg.Done()
		for i := 0; i < 1000; i++ {
			stats := d.GetStats()
			// 不应出现负值
			if stats.Downloaded < 0 {
				t.Errorf("Downloaded 出现负值: %d", stats.Downloaded)
			}
			if stats.TotalBytes < 0 {
				t.Errorf("TotalBytes 出现负值: %d", stats.TotalBytes)
			}
		}
	}()

	// 等待读取完成
	readerWg.Wait()
	close(stop)
	writerWg.Wait()
}

// Bug #14: LoadConfig 中 logWarn 在 Logger 初始化前调用
func TestLoadConfigWarnsWithoutLogger(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// 写入无效 JSON
	os.WriteFile(configPath, []byte("{bad"), 0644)

	origDir := executableDir
	executableDir = tmpDir
	defer func() { executableDir = origDir }()

	// 确保 globalLogger 为 nil
	origLogger := globalLogger
	globalLogger = nil
	defer func() { globalLogger = origLogger }()

	// 不应 panic
	cfg := LoadConfig()
	if cfg.Concurrency != 3 {
		t.Errorf("无效 JSON + 无 Logger 时应返回默认值 3，实际为 %d", cfg.Concurrency)
	}
}

// Bug #15: waitForRateLimit 并发安全
func TestRateLimitConcurrentSafety(t *testing.T) {
	client := NewClient("test-key", "")

	var wg sync.WaitGroup
	// 并发调用 waitForRateLimit 不应 panic 或 data race
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.waitForRateLimit()
		}()
	}
	wg.Wait()
}
