// Package notify is the OS-native notifier (PRISMCONDUCTOR_PLAN.md §15.6). Stub.
package notify

import (
	"os/exec"
	"runtime"
)

// Send shows an OS notification. macOS uses osascript; other platforms TODO.
func Send(title, body string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	script := `display notification "` + escape(body) + `" with title "` + escape(title) + `"`
	return exec.Command("osascript", "-e", script).Run()
}

func escape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return string(out)
}
