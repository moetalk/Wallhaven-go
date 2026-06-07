package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

const banner = `
    __          __        _  _  _                                  ____     __
   \ \        / /       | || || |                                |  __ \   / /
    \ \  /\  / /   __ _ | || || |__    __ _ __   __  ___  _ __   | |  | | / /
     \ \/  \/ /   / _` + "`" + ` || || || '_ \  / _` + "`" + ` |\ \ / / / _ \| '_ \  | |  | |/ /
      \  /\  /   | (_| || || || | | || (_| | \ V / |  __/| | | | | |__| / /___
       \/  \/     \__,_||_||_||_| |_| \__,_|  \_/   \___||_| |_| |_____/ /_____/
`

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1)

	focusedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4"))

	blurredStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6CB6FF"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#585858"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)
)

type phase int

const (
	phaseInput phase = iota
	phaseFetching
	phaseDownloading
	phaseDone
)

type fetchDoneMsg struct {
	wallpapers []*Wallpaper
	details    []*Wallpaper
	err        error
}

type downloadTickMsg struct{}

type downloadDoneMsg struct {
	stats Stats
}

type model struct {
	phase phase

	urlInput    textinput.Model
	pagesInput  textinput.Model
	apiKeyInput textinput.Model
	proxyInput  textinput.Model
	focusIndex  int
	errMsg      string

	spinner     spinner.Model
	fetchStatus string

	progress   progress.Model
	dlStats    Stats
	dlSpinner  spinner.Model
	dlRunning  bool

	finalStats Stats

	cfg        *Config
	client     WallhavenAPI
	downloader WallpaperDownloader
	width      int
	height     int
}

func initialModel(cfg *Config) model {
	u := textinput.New()
	u.Placeholder = "https://wallhaven.cc/search?categories=111&purity=111&atleast=2560x1440&sorting=toplist&topRange=1M&order=desc"
	u.Focus()
	u.CharLimit = 500
	u.Width = 60

	p := textinput.New()
	p.Placeholder = "5"
	p.CharLimit = 3
	p.Width = 10

	a := textinput.New()
	a.Placeholder = "留空跳过（仅 SFW）"
	a.CharLimit = 50
	a.Width = 40
	if cfg.APIKey != "" {
		a.SetValue(cfg.APIKey)
	}

	pr := textinput.New()
	pr.Placeholder = "http://127.0.0.1:7890"
	pr.CharLimit = 100
	pr.Width = 40
	if cfg.Proxy != "" {
		pr.SetValue(cfg.Proxy)
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	prog := progress.New(progress.WithGradient("#7D56F4", "#04B575"))
	prog.Width = 40

	ds := spinner.New()
	ds.Spinner = spinner.Dot
	ds.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))

	return model{
		phase:       phaseInput,
		urlInput:    u,
		pagesInput:  p,
		apiKeyInput: a,
		proxyInput:  pr,
		focusIndex:  0,
		spinner:     s,
		progress:    prog,
		dlSpinner:   ds,
		cfg:         cfg,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))) {
			if m.downloader != nil {
				m.downloader.Cancel()
			}
			return m, tea.Quit
		}

		switch m.phase {
		case phaseInput:
			return m.updateInput(msg)
		case phaseDone:
			if msg.String() == "q" || msg.String() == "esc" {
				return m, tea.Quit
			}
			if msg.String() == "r" {
				nm := initialModel(m.cfg)
				nm.width = m.width
				nm.height = m.height
				return nm, textinput.Blink
			}
		}

	case fetchDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.phase = phaseInput
			return m, nil
		}
		if len(msg.wallpapers) == 0 {
			m.errMsg = "未找到任何壁纸"
			m.phase = phaseInput
			return m, nil
		}
		tasks := BuildDownloadTasks(msg.wallpapers, msg.details, m.cfg.DownloadDir)
		m.downloader.SetTotal(int32(len(tasks)))
		m.dlStats.Total = int32(len(tasks))
		m.phase = phaseDownloading
		m.dlRunning = true
		go m.downloader.DownloadAll(tasks)
		return m, tea.Batch(m.dlSpinner.Tick, m.tickDownload())

	case downloadTickMsg:
		if !m.dlRunning {
			return m, nil
		}
		m.dlStats = m.downloader.GetStats()
		done := m.dlStats.Downloaded + m.dlStats.Skipped + m.dlStats.Failed
		total := m.dlStats.Total
		if total > 0 {
			cmd := m.progress.SetPercent(float64(done) / float64(total))
			if done >= total {
				m.dlRunning = false
				m.finalStats = m.dlStats
				m.phase = phaseDone
				return m, cmd
			}
			return m, tea.Batch(cmd, m.dlSpinner.Tick, m.tickDownload())
		}
		return m, tea.Batch(m.dlSpinner.Tick, m.tickDownload())

	case downloadDoneMsg:
		m.finalStats = msg.stats
		m.dlRunning = false
		m.phase = phaseDone
		return m, nil

	case spinner.TickMsg:
		if m.phase == phaseFetching {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		if m.phase == phaseDownloading {
			var cmd tea.Cmd
			m.dlSpinner, cmd = m.dlSpinner.Update(msg)
			return m, cmd
		}

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	if m.phase == phaseFetching {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	if m.phase == phaseDownloading {
		var cmd tea.Cmd
		m.dlSpinner, cmd = m.dlSpinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) tickDownload() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return downloadTickMsg{}
	})
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "shift+tab", "up", "down":
		if msg.String() == "up" || msg.String() == "shift+tab" {
			m.focusIndex--
		} else {
			m.focusIndex++
		}
		if m.focusIndex > 4 {
			m.focusIndex = 0
		}
		if m.focusIndex < 0 {
			m.focusIndex = 4
		}
		cmds := make([]tea.Cmd, 4)
		inputs := []*textinput.Model{&m.urlInput, &m.pagesInput, &m.apiKeyInput, &m.proxyInput}
		for i, inp := range inputs {
			if i == m.focusIndex {
				cmds[i] = inp.Focus()
			} else {
				inp.Blur()
			}
		}
		return m, tea.Batch(cmds...)

	case "enter":
		if m.focusIndex == 4 {
			return m.startFetch()
		}
		m.focusIndex++
		if m.focusIndex > 4 {
			m.focusIndex = 4
		}
		cmds := make([]tea.Cmd, 4)
		inputs := []*textinput.Model{&m.urlInput, &m.pagesInput, &m.apiKeyInput, &m.proxyInput}
		for i, inp := range inputs {
			if i == m.focusIndex {
				cmds[i] = inp.Focus()
			} else {
				inp.Blur()
			}
		}
		return m, tea.Batch(cmds...)
	}

	switch m.focusIndex {
	case 0:
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)
		return m, cmd
	case 1:
		var cmd tea.Cmd
		m.pagesInput, cmd = m.pagesInput.Update(msg)
		return m, cmd
	case 2:
		var cmd tea.Cmd
		m.apiKeyInput, cmd = m.apiKeyInput.Update(msg)
		return m, cmd
	case 3:
		var cmd tea.Cmd
		m.proxyInput, cmd = m.proxyInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) startFetch() (tea.Model, tea.Cmd) {
	urlVal := strings.TrimSpace(m.urlInput.Value())
	if urlVal == "" {
		m.errMsg = "请输入搜索 URL"
		return m, nil
	}

	pagesVal := 5
	if v := strings.TrimSpace(m.pagesInput.Value()); v != "" {
		_, err := fmt.Sscanf(v, "%d", &pagesVal)
		if err != nil || pagesVal <= 0 {
			m.errMsg = "页数必须为正整数"
			return m, nil
		}
	}

	if apiKey := strings.TrimSpace(m.apiKeyInput.Value()); apiKey != "" {
		m.cfg.APIKey = apiKey
		m.cfg.Save()
	}
	if proxy := strings.TrimSpace(m.proxyInput.Value()); proxy != "" {
		m.cfg.Proxy = proxy
		m.cfg.Save()
	}

	m.errMsg = ""
	m.phase = phaseFetching
	m.fetchStatus = "正在连接 Wallhaven API..."

	client := NewClient(m.cfg.APIKey, m.cfg.Proxy)
	m.client = client
	m.downloader = NewDownloader(client.HTTPClient(), m.cfg)

	return m, tea.Batch(m.spinner.Tick, m.doFetch(urlVal, pagesVal))
}

func (m model) doFetch(urlStr string, pages int) tea.Cmd {
	return func() tea.Msg {
		singleID := extractSingleWallpaperID(urlStr)
		if singleID != "" {
			detail, err := m.client.GetWallpaperDetail(singleID)
			if err != nil {
				return fetchDoneMsg{err: err}
			}
			return fetchDoneMsg{
				wallpapers: []*Wallpaper{detail},
				details:    []*Wallpaper{detail},
			}
		}

		params, err := ParseSearchURL(urlStr)
		if err != nil {
			return fetchDoneMsg{err: err}
		}

		allWP, allDetails, err := FetchWallpapers(m.client, params, pages, m.downloader.IsCancelled)
		if err != nil {
			return fetchDoneMsg{err: err}
		}

		return fetchDoneMsg{wallpapers: allWP, details: allDetails}
	}
}

func (m model) View() string {
	switch m.phase {
	case phaseInput:
		return m.viewInput()
	case phaseFetching:
		return m.viewFetching()
	case phaseDownloading:
		return m.viewDownloading()
	case phaseDone:
		return m.viewDone()
	}
	return ""
}

func (m model) viewInput() string {
	b := strings.TrimLeft(banner, "\n")
	s := titleStyle.Render(b) + "\n\n"

	inputs := []struct {
		label   string
		input   textinput.Model
		focused bool
	}{
		{"搜索 URL", m.urlInput, m.focusIndex == 0},
		{"下载页数", m.pagesInput, m.focusIndex == 1},
		{"API Key", m.apiKeyInput, m.focusIndex == 2},
		{"代理地址", m.proxyInput, m.focusIndex == 3},
	}

	for _, inp := range inputs {
		style := blurredStyle
		cursor := "  "
		if inp.focused {
			style = focusedStyle
			cursor = "> "
		}
		s += style.Render(fmt.Sprintf("%s%s: ", cursor, inp.label))
		s += inp.input.View()
		s += "\n\n"
	}

	startStyle := blurredStyle
	startCursor := "  "
	if m.focusIndex == 4 {
		startStyle = focusedStyle
		startCursor = "> "
	}
	s += startStyle.Render(fmt.Sprintf("%s🚀 开始下载", startCursor))
	s += "\n\n"

	if m.errMsg != "" {
		s += errorStyle.Render("  ✗ " + m.errMsg) + "\n\n"
	}

	s += hintStyle.Render("  Tab/↑↓ 切换字段 | Enter 确认/开始 | Ctrl+C 退出")
	s += "\n"
	s += hintStyle.Render("  下载目录: " + m.cfg.DownloadDir)

	return boxStyle.Render(s)
}

func (m model) viewFetching() string {
	b := strings.TrimLeft(banner, "\n")
	s := titleStyle.Render(b) + "\n\n"
	s += m.spinner.View() + " " + infoStyle.Render(m.fetchStatus)
	s += "\n\n"
	s += hintStyle.Render("  Ctrl+C 取消")
	return boxStyle.Render(s)
}

func (m model) viewDownloading() string {
	b := strings.TrimLeft(banner, "\n")
	s := titleStyle.Render(b) + "\n\n"

	s += infoStyle.Render("  📥 正在下载壁纸...") + "\n\n"

	s += m.progress.View() + "\n\n"

	done := m.dlStats.Downloaded + m.dlStats.Skipped + m.dlStats.Failed
	total := m.dlStats.Total
	pct := float64(0)
	if total > 0 {
		pct = float64(done) / float64(total) * 100
	}

	s += fmt.Sprintf("  进度: %d/%d (%.1f%%)\n", done, total, pct)
	s += successStyle.Render(fmt.Sprintf("  ✓ 已下载: %d", m.dlStats.Downloaded)) + "  "
	s += infoStyle.Render(fmt.Sprintf("○ 已跳过: %d", m.dlStats.Skipped)) + "  "
	if m.dlStats.Failed > 0 {
		s += errorStyle.Render(fmt.Sprintf("✗ 失败: %d", m.dlStats.Failed))
	}
	s += "\n\n"
	s += m.dlSpinner.View() + " " + hintStyle.Render("下载中...")
	s += "\n\n"
	s += hintStyle.Render("  Ctrl+C 优雅中断")

	return boxStyle.Render(s)
}

func (m model) viewDone() string {
	b := strings.TrimLeft(banner, "\n")
	s := titleStyle.Render(b) + "\n\n"

	s += successStyle.Render("  ═══════════════════════════════════════") + "\n"
	s += successStyle.Render("    下载完成") + "\n"
	s += successStyle.Render("  ═══════════════════════════════════════") + "\n\n"

	s += fmt.Sprintf("    总计:     %d\n", m.finalStats.Total)
	s += successStyle.Render(fmt.Sprintf("    已下载:   %d (%s)\n", m.finalStats.Downloaded, formatBytes(m.finalStats.TotalBytes)))
	s += infoStyle.Render(fmt.Sprintf("    已跳过:   %d（文件已存在）\n", m.finalStats.Skipped))
	if m.finalStats.Failed > 0 {
		s += errorStyle.Render(fmt.Sprintf("    失败:     %d\n", m.finalStats.Failed))
	}
	s += "\n"
	s += hintStyle.Render("  r 重新下载 | q/Esc 退出")

	return boxStyle.Render(s)
}

func runTUI(cfg *Config) error {
	SetTUIMode(true)
	CleanTmpFiles(cfg.DownloadDir)
	p := tea.NewProgram(initialModel(cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
