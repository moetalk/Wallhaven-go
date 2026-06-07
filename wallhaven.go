package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://wallhaven.cc/api/v1"

type Wallpaper struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	ShortURL   string `json:"short_url"`
	Views      int    `json:"views"`
	Favorites  int    `json:"favorites"`
	Source     string `json:"source"`
	Purity     string `json:"purity"`
	Category   string `json:"category"`
	DimensionX int    `json:"dimension_x"`
	DimensionY int    `json:"dimension_y"`
	Resolution string `json:"resolution"`
	Ratio      string `json:"ratio"`
	FileSize   int    `json:"file_size"`
	FileType   string `json:"file_type"`
	CreatedAt  string `json:"created_at"`
	Path       string `json:"path"`
}

type WallpaperDetail struct {
	Data Wallpaper `json:"data"`
}

type SearchMeta struct {
	CurrentPage int    `json:"current_page"`
	LastPage    int    `json:"last_page"`
	PerPage     int    `json:"per_page,string"`
	Total       int    `json:"total"`
}

type SearchResult struct {
	Data []Wallpaper `json:"data"`
	Meta SearchMeta  `json:"meta"`
}

type SearchParams struct {
	Query       string
	Categories  string
	Purity      string
	AtLeast     string
	Resolutions string
	Ratios      string
	Colors      string
	Sorting     string
	TopRange    string
	Order       string
	Page        int
	Seed        string
}

type WallhavenAPI interface {
	Search(params *SearchParams, page int) (*SearchResult, error)
	GetWallpaperDetail(id string) (*Wallpaper, error)
}

type Client struct {
	apiKey     string
	httpClient *http.Client
	lastReqs   []time.Time
	mu         sync.Mutex
}

func buildTransport(proxyURL string) (http.RoundTripper, error) {
	transport := &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		Proxy:               http.ProxyFromEnvironment,
	}

	if proxyURL != "" {
		proxyParsed, err := url.Parse(proxyURL)
		if err != nil {
			return transport, fmt.Errorf("配置文件中的代理地址无效: %s (%v)，将使用系统代理", proxyURL, err)
		}
		transport.Proxy = http.ProxyURL(proxyParsed)
	}

	return transport, nil
}

func NewClient(apiKey string, proxyURL string) *Client {
	transport, proxyErr := buildTransport(proxyURL)
	if proxyErr != nil {
		logWarn("%s", proxyErr.Error())
	} else if proxyURL != "" {
		logInfo("使用配置文件代理: %s", proxyURL)
	}

	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

func ParseSearchURL(rawURL string) (*SearchParams, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL 格式无效，请检查是否完整输入了 Wallhaven 搜索页面地址")
	}

	if parsed.Host != "" && parsed.Host != "wallhaven.cc" && !strings.HasSuffix(parsed.Host, "wallhaven.cc") {
		return nil, fmt.Errorf("不支持的网站地址 (%s)，本工具仅支持 wallhaven.cc", parsed.Host)
	}

	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) >= 2 && pathParts[0] == "w" {
		return nil, &SingleWallpaperError{ID: pathParts[1]}
	}

	params := &SearchParams{}

	q := parsed.Query()
	params.Query = q.Get("q")
	params.Categories = q.Get("categories")
	params.Purity = q.Get("purity")
	params.AtLeast = q.Get("atleast")
	params.Resolutions = q.Get("resolutions")
	params.Ratios = q.Get("ratios")
	params.Colors = q.Get("colors")
	params.Sorting = q.Get("sorting")
	params.TopRange = q.Get("topRange")
	params.Order = q.Get("order")
	params.Seed = q.Get("seed")

	if p := q.Get("page"); p != "" {
		params.Page, _ = strconv.Atoi(p)
	}

	return params, nil
}

func (c *Client) waitForRateLimit() {
	const maxReqsPerMinute = 40
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Minute)

	valid := make([]time.Time, 0, len(c.lastReqs))
	for _, t := range c.lastReqs {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	c.lastReqs = valid

	if len(valid) >= maxReqsPerMinute {
		waitDur := time.Minute - now.Sub(valid[0])
		if waitDur > 0 {
			c.mu.Unlock()
			logDebug("速率限制: 等待 %v", waitDur.Round(time.Second))
			time.Sleep(waitDur + time.Second)
			c.mu.Lock()
		}
	}

	c.lastReqs = append(c.lastReqs, time.Now())
}

func (c *Client) doRequest(reqURL string) ([]byte, error) {
	return c.doRequestWithRetry(reqURL, 3)
}

func (c *Client) doRequestWithRetry(reqURL string, maxRetries int) ([]byte, error) {
	c.waitForRateLimit()

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}

	req.Header.Set("User-Agent", "Wallhaven-DL/1.0")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	logDebug("请求: GET %s", reqURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "proxyconnect") {
			return nil, fmt.Errorf("代理连接失败，请检查代理设置或网络环境: %w", err)
		}
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			return nil, fmt.Errorf("请求超时，网络可能不稳定，请稍后重试: %w", err)
		}
		if strings.Contains(err.Error(), "connection refused") {
			return nil, fmt.Errorf("连接被拒绝，Wallhaven 服务可能暂时不可用: %w", err)
		}
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	logDebug("响应: HTTP %d (%s)", resp.StatusCode, reqURL)

	if resp.StatusCode == 429 {
		if maxRetries <= 0 {
			return nil, fmt.Errorf("API 速率限制已触发多次，请稍等几分钟后再试")
		}
		logWarn("收到 429 速率限制，等待 60 秒后重试（剩余重试: %d）", maxRetries)
		printInfoLn("  ⏳ 触发速率限制 (429)，等待 60 秒后重试...")
		time.Sleep(60 * time.Second)
		return c.doRequestWithRetry(reqURL, maxRetries-1)
	}

	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("API Key 认证失败 (401)，请检查 Key 是否正确或是否已过期")
	}

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("请求的资源不存在 (404)，壁纸可能已被删除")
	}

	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("访问被拒绝 (403)，可能需要有效的 API Key 才能访问此内容")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		logDebug("错误响应体: %s", string(body))
		return nil, fmt.Errorf("API 返回错误 (HTTP %d)，请稍后重试", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) Search(params *SearchParams, page int) (*SearchResult, error) {
	u, _ := url.Parse(apiBase + "/search")
	q := u.Query()

	if params.Query != "" {
		q.Set("q", params.Query)
	}
	if params.Categories != "" {
		q.Set("categories", params.Categories)
	}
	if params.Purity != "" {
		q.Set("purity", params.Purity)
	}
	if params.AtLeast != "" {
		q.Set("atleast", params.AtLeast)
	}
	if params.Resolutions != "" {
		q.Set("resolutions", params.Resolutions)
	}
	if params.Ratios != "" {
		q.Set("ratios", params.Ratios)
	}
	if params.Colors != "" {
		q.Set("colors", params.Colors)
	}
	if params.Sorting != "" {
		q.Set("sorting", params.Sorting)
	}
	if params.TopRange != "" {
		q.Set("topRange", params.TopRange)
	}
	if params.Order != "" {
		q.Set("order", params.Order)
	}
	if params.Seed != "" {
		q.Set("seed", params.Seed)
	}
	q.Set("page", strconv.Itoa(page))

	if c.apiKey != "" {
		q.Set("apikey", c.apiKey)
	}

	u.RawQuery = q.Encode()

	data, err := c.doRequest(u.String())
	if err != nil {
		return nil, fmt.Errorf("搜索第 %d 页失败: %w", page, err)
	}

	var result SearchResult
	if err := json.Unmarshal(data, &result); err != nil {
		logDebug("搜索响应原文: %s", string(data))
		return nil, fmt.Errorf("搜索结果解析失败，API 可能返回了非预期格式")
	}

	logDebug("搜索第 %d 页: 获取到 %d 张壁纸，共 %d 张/ %d 页", page, len(result.Data), result.Meta.Total, result.Meta.LastPage)
	return &result, nil
}

func (c *Client) GetWallpaperDetail(id string) (*Wallpaper, error) {
	apiURL := apiBase + "/w/" + id
	if c.apiKey != "" {
		apiURL += "?apikey=" + c.apiKey
	}

	data, err := c.doRequest(apiURL)
	if err != nil {
		return nil, fmt.Errorf("获取壁纸 %s 详情失败: %w", id, err)
	}

	var detail WallpaperDetail
	if err := json.Unmarshal(data, &detail); err != nil {
		logDebug("壁纸详情响应原文 (%s): %s", id, string(data))
		return nil, fmt.Errorf("壁纸 %s 详情解析失败", id)
	}

	logDebug("壁纸详情 %s: %s (%s, %s)", id, detail.Data.Path, detail.Data.Resolution, detail.Data.Category)
	return &detail.Data, nil
}

func CategoryDirName(category string) string {
	switch strings.ToLower(category) {
	case "anime":
		return "anime"
	case "people":
		return "people"
	case "general":
		return "general"
	default:
		return "other"
	}
}

func FileExtFromURL(imageURL string) string {
	parsed, err := url.Parse(imageURL)
	if err != nil {
		return ".jpg"
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	if ext == "" {
		return ".jpg"
	}
	return ext
}

// SingleWallpaperError 表示用户输入的是单张壁纸 URL 而非搜索 URL
type SingleWallpaperError struct {
	ID string
}

func (e *SingleWallpaperError) Error() string {
	return fmt.Sprintf("单张壁纸: %s", e.ID)
}

func isFullPath(path string) bool {
	return strings.Contains(path, "/full/")
}

func FetchWallpapers(client WallhavenAPI, params *SearchParams, pages int, cancelCheck func() bool) ([]*Wallpaper, []*Wallpaper, error) {
	var allWP []*Wallpaper
	var allDetails []*Wallpaper

	for page := 1; page <= pages; page++ {
		if cancelCheck != nil && cancelCheck() {
			break
		}
		result, err := client.Search(params, page)
		if err != nil {
			return allWP, allDetails, fmt.Errorf("搜索第 %d 页失败: %w", page, err)
		}
		if len(result.Data) == 0 {
			break
		}

		for i := range result.Data {
			allWP = append(allWP, &result.Data[i])
			if isFullPath(result.Data[i].Path) {
				allDetails = append(allDetails, &result.Data[i])
			} else {
				logWarn("壁纸 %s 的路径不包含 /full/，正在获取完整路径", result.Data[i].ID)
				detail, err := client.GetWallpaperDetail(result.Data[i].ID)
				if err != nil {
					logWarn("获取壁纸 %s 详情失败: %v", result.Data[i].ID, err)
					allDetails = append(allDetails, &result.Data[i])
				} else {
					allDetails = append(allDetails, detail)
				}
			}
		}

		if result.Meta.LastPage > 0 && page >= result.Meta.LastPage {
			break
		}
	}

	return allWP, allDetails, nil
}
