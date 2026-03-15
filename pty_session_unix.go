//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type ptySession struct {
	cmd  *exec.Cmd
	ptmx *os.File
}

func newPTYSession(name string, args []string, cols, rows uint16) (*ptySession, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	winSize := &pty.Winsize{Cols: cols, Rows: rows}
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		return nil, err
	}

	return &ptySession{cmd: cmd, ptmx: ptmx}, nil
}

func (s *ptySession) Read(p []byte) (int, error) {
	return s.ptmx.Read(p)
}

func (s *ptySession) Write(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

func (s *ptySession) Resize(cols, rows uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

func (s *ptySession) Pid() int {
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

func (s *ptySession) IsRunning() bool {
	if s.cmd.Process == nil {
		return false
	}
	err := s.cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

func (s *ptySession) Kill() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGTERM)
	}
}

func (s *ptySession) Wait() {
	_ = s.cmd.Wait()
}

// Shutdown releases resources to unblock readLoop after process exit.
// On Unix this is a no-op; the PTY master returns EOF when the child exits.
func (s *ptySession) Shutdown() {}

func (s *ptySession) Close() {
	_ = s.ptmx.Close()
}
