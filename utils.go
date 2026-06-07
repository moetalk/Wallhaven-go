package main

import (
	"os"
	"runtime"
	"strings"
)

func isSingleWallpaperURL(urlStr string) bool {
	return extractSingleWallpaperID(urlStr) != ""
}

func extractSingleWallpaperID(urlStr string) string {
	if urlStr == "" {
		return ""
	}
	idx := strings.Index(urlStr, "wallhaven.cc/w/")
	if idx == -1 {
		return ""
	}
	rest := urlStr[idx+len("wallhaven.cc/w/"):]
	// 去除查询参数和路径分隔符
	if qIdx := strings.Index(rest, "?"); qIdx != -1 {
		rest = rest[:qIdx]
	}
	if sIdx := strings.Index(rest, "/"); sIdx != -1 {
		rest = rest[:sIdx]
	}
	// 去除锚点
	if hIdx := strings.Index(rest, "#"); hIdx != -1 {
		rest = rest[:hIdx]
	}
	id := rest
	if len(id) >= 4 && len(id) <= 10 {
		return id
	}
	return ""
}

func categoryLabel(c string) string {
	if len(c) < 3 {
		return c
	}
	labels := []string{}
	if c[0] == '1' {
		labels = append(labels, "General")
	}
	if c[1] == '1' {
		labels = append(labels, "Anime")
	}
	if c[2] == '1' {
		labels = append(labels, "People")
	}
	return strings.Join(labels, " + ")
}

func purityLabel(p string) string {
	if len(p) < 3 {
		return p
	}
	labels := []string{}
	if p[0] == '1' {
		labels = append(labels, "SFW")
	}
	if p[1] == '1' {
		labels = append(labels, "Sketchy")
	}
	if p[2] == '1' {
		labels = append(labels, "NSFW")
	}
	return strings.Join(labels, " + ")
}

func sortingLabel(s string) string {
	labels := map[string]string{
		"date_added": "最新添加",
		"relevance":  "相关性",
		"random":     "随机",
		"views":      "浏览量",
		"favorites":  "收藏数",
		"toplist":    "热门排行",
	}
	if label, ok := labels[s]; ok {
		return label
	}
	return s
}

func topRangeLabel(r string) string {
	labels := map[string]string{
		"1d": "1天",
		"3d": "3天",
		"1w": "1周",
		"1M": "1个月",
		"3M": "3个月",
		"6M": "6个月",
		"1y": "1年",
	}
	if label, ok := labels[r]; ok {
		return label
	}
	return r
}

func supportsUnicode() bool {
	term := os.Getenv("TERM")
	unicodeTerms := []string{
		"xterm-256color", "xterm", "screen-256color", "screen",
		"tmux-256color", "tmux", "rxvt-unicode", "rxvt",
		"alacritty", "kitty", "wezterm", "foot", "st-256color",
	}
	for _, t := range unicodeTerms {
		if term == t {
			return true
		}
	}
	if os.Getenv("WT_SESSION") != "" {
		return true
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return false
	}
	return true
}
