package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	file    *os.File
	logger  *log.Logger
	mu      sync.Mutex
	verbose bool
}

var globalLogger *Logger

var tuiMode bool

func SetTUIMode(mode bool) {
	tuiMode = mode
}

func IsTUIMode() bool {
	return tuiMode
}

func InitLogger(verbose bool) error {
	logPath := filepath.Join(executableDir, "wallhaven-dl.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("无法创建日志文件 %s: %w", logPath, err)
	}

	size, _ := f.Seek(0, io.SeekEnd)
	if size > 2*1024*1024 {
		f.Close()
		backupPath := logPath + ".old"
		os.Rename(logPath, backupPath)
		f, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("无法创建日志文件: %w", err)
		}
	}

	globalLogger = &Logger{
		file:    f,
		logger:  log.New(f, "", 0),
		verbose: verbose,
	}

	globalLogger.write("INFO", "═══════════════════════════════════════")
	globalLogger.write("INFO", "wallhaven-dl v%s 启动", version)
	globalLogger.write("INFO", "时间: %s", time.Now().Format("2006-01-02 15:04:05"))
	globalLogger.write("INFO", "═══════════════════════════════════════")

	return nil
}

func CloseLogger() {
	if globalLogger != nil && globalLogger.file != nil {
		globalLogger.write("INFO", "wallhaven-dl 关闭")
		globalLogger.file.Close()
	}
}

func GetLogger() *Logger {
	if globalLogger == nil {
		return &Logger{
			logger: log.New(io.Discard, "", 0),
		}
	}
	return globalLogger
}

func (l *Logger) write(level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05.000")
	l.logger.Printf("[%s] [%s] %s", timestamp, level, msg)
}

func logDebug(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.write("DEBUG", format, args...)
		if globalLogger.verbose {
			msg := fmt.Sprintf(format, args...)
			fmt.Printf("  [DEBUG] %s\n", msg)
		}
	}
}

func logInfo(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.write("INFO", format, args...)
	}
}

func logWarn(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.write("WARN", format, args...)
		if globalLogger.verbose {
			msg := fmt.Sprintf(format, args...)
			fmt.Printf("  [WARN] %s\n", msg)
		}
	}
}

func logError(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.write("ERROR", format, args...)
	}
}

func printInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !tuiMode {
		fmt.Print(msg)
	}
	if globalLogger != nil {
		globalLogger.write("INFO", "%s", msg)
	}
}

func printInfoLn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !tuiMode {
		fmt.Println(msg)
	}
	if globalLogger != nil {
		globalLogger.write("INFO", "%s", msg)
	}
}
