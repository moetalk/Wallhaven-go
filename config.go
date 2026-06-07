package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var executableDir string

func init() {
	exe, err := os.Executable()
	if err != nil {
		executableDir, _ = os.Getwd()
	} else {
		executableDir = filepath.Dir(exe)
	}
}

type Config struct {
	Comment     string `json:"_comment"`
	ExeDir      string `json:"-"`
	APIKey      string `json:"api_key"`
	DownloadDir string `json:"download_dir"`
	Concurrency int    `json:"concurrency"`
	RetryCount  int    `json:"retry_count"`
	Proxy       string `json:"proxy"`
}

func defaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		Comment:     "Wallhaven-DL 配置文件 | API Key 获取: https://wallhaven.cc/settings/account",
		ExeDir:      executableDir,
		APIKey:      "",
		DownloadDir: filepath.Join(homeDir, "Wallhaven"),
		Concurrency: 3,
		RetryCount:  3,
		Proxy:       "",
	}
}

func (c *Config) configFilePath() string {
	return filepath.Join(c.ExeDir, "config.json")
}

func (c *Config) Exists() bool {
	_, err := os.Stat(c.configFilePath())
	return err == nil
}

func LoadConfig() *Config {
	cfg := defaultConfig()
	path := cfg.configFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		// Logger 可能尚未初始化，同时输出到 stderr 和日志
		fmt.Fprintf(os.Stderr, "警告: 配置文件格式无效，使用默认值: %v\n", err)
		logWarn("配置文件格式无效，使用默认值: %v", err)
		return cfg
	}
	// 校验并修正配置值
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 3
	}
	if cfg.RetryCount < 1 {
		cfg.RetryCount = 3
	}
	return cfg
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.configFilePath(), data, 0644)
}

func (c *Config) EnsureAPIKey() {
	if c.APIKey != "" {
		return
	}
	fmt.Println()
	fmt.Println("  ⚙ 首次运行配置")
	fmt.Println("  ─────────────────────────────────────────")
	fmt.Println("  Wallhaven API Key 用于访问 NSFW 内容和提升速率限制。")
	fmt.Println("  获取地址: https://wallhaven.cc/settings/account")
	fmt.Println("  如不需 NSFW 内容，可直接按 Enter 跳过。")
	fmt.Println()
	fmt.Print("  请输入 API Key（留空跳过）: ")

	reader := bufio.NewReader(os.Stdin)
	key, _ := reader.ReadString('\n')
	key = strings.TrimSpace(key)

	if key != "" {
		c.APIKey = key
		fmt.Println("  ✓ API Key 已保存。")
	} else {
		fmt.Println("  ○ 已跳过 API Key，仅可下载 SFW 内容。")
	}

	c.Save()
	fmt.Println()
}
