package conn

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/aymanbagabas/go-pty"
)

// LocalShell is the default shell on this computer, as an absolute path (the
// pseudo terminal does not search PATH): PowerShell 7 if
// installed, else Windows PowerShell; $SHELL or bash elsewhere.
func LocalShell() (name string, args []string) {
	if runtime.GOOS == "windows" {
		for _, exe := range []string{"pwsh.exe", "powershell.exe"} {
			if p, err := exec.LookPath(exe); err == nil {
				return p, []string{"-NoLogo"}
			}
		}
		return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), []string{"-NoLogo"}
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		if p, err := exec.LookPath(sh); err == nil {
			return p, []string{"-l"}
		}
	}
	return "/bin/bash", []string{"-l"}
}

type local struct {
	pty  pty.Pty
	cmd  *pty.Cmd
	stop once
}

// OpenLocal starts the local shell in a pseudo terminal (ConPTY on Windows).
func OpenLocal(cols, rows int, h Handlers) (Session, error) {
	p, err := pty.New()
	if err != nil {
		return nil, err
	}
	if cols > 0 && rows > 0 {
		p.Resize(cols, rows)
	}
	name, args := LocalShell()
	cmd := p.Command(name, args...)
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if err := cmd.Start(); err != nil {
		p.Close()
		return nil, err
	}
	s := &local{pty: p, cmd: cmd}
	go pump(p, h, func() { s.Close() })
	go func() {
		cmd.Wait()
		s.Close() // closing the pty ends the reader, which reports the end
	}()
	return s, nil
}

func (s *local) Write(b []byte) (int, error) { return s.pty.Write(b) }
func (s *local) Resize(cols, rows int) error { return s.pty.Resize(cols, rows) }
func (s *local) Close() error {
	return s.stop.do(func() error {
		if s.cmd.Process != nil {
			s.cmd.Process.Kill()
		}
		return s.pty.Close()
	})
}
