package gitpack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const installLogName = "skill-manager-install.log"

var installLog = struct {
	mu       sync.Mutex
	on       bool
	override string
}{}

// InstallLogFile 是 install 追加写下的日志。默认在系统临时目录。
func InstallLogFile() string {
	installLog.mu.Lock()
	defer installLog.mu.Unlock()
	if installLog.override != "" {
		return installLog.override
	}
	return filepath.Join(os.TempDir(), installLogName)
}

// SetInstallLogForTest 替换日志文件。传入空字符串恢复默认。
func SetInstallLogForTest(path string) {
	installLog.mu.Lock()
	installLog.override = path
	installLog.mu.Unlock()
}

// BeginInstallLog 让随后的 AppendInstallLog 写入日志。与 EndInstallLog 成对。
func BeginInstallLog() {
	installLog.mu.Lock()
	installLog.on = true
	installLog.mu.Unlock()
}

// EndInstallLog 停止写入。
func EndInstallLog() {
	installLog.mu.Lock()
	installLog.on = false
	installLog.mu.Unlock()
}

// AppendInstallLog 在 install 进行时追加一行或多行。未开始时不写。令牌换成 <REDACTED>。
func AppendInstallLog(text string) {
	text = strings.TrimSpace(redactInstallLog(text))
	if text == "" {
		return
	}
	if len(text) > 4000 {
		text = text[:4000] + "\n…"
	}
	installLog.mu.Lock()
	defer installLog.mu.Unlock()
	if !installLog.on {
		return
	}
	path := installLog.override
	if path == "" {
		path = filepath.Join(os.TempDir(), installLogName)
	}
	stamp := time.Now().Format("2006-01-02 15:04:05")
	var b strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i == 0 {
			fmt.Fprintf(&b, "%s %s\n", stamp, line)
			continue
		}
		fmt.Fprintf(&b, "  %s\n", line)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(b.String())
	_ = f.Close()
}

func redactInstallLog(text string) string {
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		value := os.Getenv(key)
		if len(value) < 8 {
			continue
		}
		text = strings.ReplaceAll(text, value, "<REDACTED>")
	}
	return text
}
