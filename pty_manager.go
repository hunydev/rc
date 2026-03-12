package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// PTYManager manages a process running in a pseudo-terminal.
type PTYManager struct {
	cmdName  string
	cmdArgs  []string
	initCols uint16
	initRows uint16
	curCols  uint16
	curRows  uint16
	cmd      *exec.Cmd
	ptmx     *os.File
	buf      *OutputBuffer
	outputCh chan []byte
	mu       sync.Mutex
	closed   bool
}

// NewPTYManager spawns a command in a PTY and starts reading its output.
func NewPTYManager(name string, args []string, cols, rows uint16, buf *OutputBuffer) (*PTYManager, error) {
	mgr := &PTYManager{
		cmdName:  name,
		cmdArgs:  args,
		initCols: cols,
		initRows: rows,
		buf:      buf,
	}

	if err := mgr.start(cols, rows); err != nil {
		return nil, err
	}

	return mgr, nil
}

// start spawns the process and begins reading.
func (m *PTYManager) start(cols, rows uint16) error {
	cmd := exec.Command(m.cmdName, m.cmdArgs...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	winSize := &pty.Winsize{Cols: cols, Rows: rows}
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}

	m.cmd = cmd
	m.ptmx = ptmx
	m.closed = false
	m.curCols = cols
	m.curRows = rows
	m.outputCh = make(chan []byte, 256)

	go m.readLoop()
	go m.waitProcess()

	log.Printf("PTY started: pid=%d, cmd=%s %v, size=%dx%d", cmd.Process.Pid, m.cmdName, m.cmdArgs, cols, rows)
	return nil
}

// Restart stops the current process and starts a new one.
// Returns the new output channel for the caller to read from.
func (m *PTYManager) Restart() (<-chan []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Kill old process
	if m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(syscall.SIGTERM)
	}
	if m.ptmx != nil {
		_ = m.ptmx.Close()
	}

	// Clear output buffer
	m.buf.Reset()

	// Start fresh with last known size (not initial size)
	cols, rows := m.curCols, m.curRows
	if cols == 0 || rows == 0 {
		cols, rows = m.initCols, m.initRows
	}
	if err := m.start(cols, rows); err != nil {
		return nil, fmt.Errorf("restart: %w", err)
	}

	log.Printf("PTY restarted: pid=%d", m.cmd.Process.Pid)
	return m.outputCh, nil
}

func (m *PTYManager) readLoop() {
	tmp := make([]byte, 4096)
	for {
		n, err := m.ptmx.Read(tmp)
		if n > 0 {
			data := make([]byte, n)
			copy(data, tmp[:n])
			m.buf.Write(data)
			m.outputCh <- data
		}
		if err != nil {
			break
		}
	}
	close(m.outputCh)
}

func (m *PTYManager) waitProcess() {
	_ = m.cmd.Wait()
	log.Printf("PTY process exited: pid=%d", m.cmd.Process.Pid)
}

// OutputChan returns a channel that receives PTY output.
func (m *PTYManager) OutputChan() <-chan []byte {
	return m.outputCh
}

// Write sends data to the PTY stdin.
func (m *PTYManager) Write(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("pty closed")
	}
	_, err := m.ptmx.Write(data)
	return err
}

// Resize changes the PTY window size.
func (m *PTYManager) Resize(cols, rows uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("pty closed")
	}
	m.curCols = cols
	m.curRows = rows
	return pty.Setsize(m.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// Pid returns the process ID.
func (m *PTYManager) Pid() int {
	if m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}
	return 0
}

// IsRunning returns whether the process is still running.
func (m *PTYManager) IsRunning() bool {
	if m.cmd.Process == nil {
		return false
	}
	err := m.cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

// Close terminates the PTY and process.
func (m *PTYManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	if m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(syscall.SIGTERM)
	}
	_ = m.ptmx.Close()
}
