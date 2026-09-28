package symlink

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func Target(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	target = strings.TrimPrefix(target, `\\?\`)
	return target
}

func Create(link, target string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return "", err
	}
	if err := os.Symlink(target, link); err == nil {
		return "symlink", nil
	} else if runtime.GOOS != "windows" {
		return "", err
	}
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil || Target(link) == "" {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = "创建目录联接失败"
		}
		return "", fmt.Errorf("%s", detail)
	}
	return "junction", nil
}

func Remove(path string) error {
	if Target(path) != "" {
		return os.Remove(path)
	}
	return os.Remove(path)
}
