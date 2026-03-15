package main

import (
	"fmt"
	"log"
	"sync"
)

// PTYManager manages a process running in a pseudo-terminal.
type PTYManager struct {
	cmdName  string
	cmdArgs  []string
	initCols uint16
	initRows uint16
	curCols  uint16
	curRows  uint16
	session  *ptySession
	buf      *OutputBuffer
	outputCh chan []byte
	extraEnv []string
	mu       sync.Mutex
	closed   bool
}

// NewPTYManager spawns a command in a PTY and starts reading its output.
func NewPTYManager(name string, args []string, cols, rows uint16, buf *OutputBuffer, extraEnv []string) (*PTYManager, error) {
	mgr := &PTYManager{
		cmdName:  name,
		cmdArgs:  args,
		initCols: cols,
		initRows: rows,
		buf:      buf,
		extraEnv: extraEnv,
	}

	if err := mgr.start(cols, rows); err != nil {
		return nil, err
	}

	return mgr, nil
}

func (m *PTYManager) start(cols, rows uint16) error {
	session, err := newPTYSession(m.cmdName, m.cmdArgs, cols, rows, m.extraEnv)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}

	m.session = session
	m.closed = false
	m.curCols = cols
	m.curRows = rows
	m.outputCh = make(chan []byte, channelBufSize)

	sess := session
	ch := m.outputCh
	safeGo("readLoop", func() { m.readLoop(sess, ch) })
	safeGo("waitProcess", func() { m.waitProcess(sess) })

	log.Printf("PTY started: pid=%d, cmd=%s %v, size=%dx%d", session.Pid(), m.cmdName, m.cmdArgs, cols, rows)
	return nil
}

// Restart stops the current process and starts a new one.
func (m *PTYManager) Restart() (<-chan []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.session.Kill()
	m.session.Close()
	m.buf.Reset()

	cols, rows := m.curCols, m.curRows
	if cols == 0 || rows == 0 {
		cols, rows = m.initCols, m.initRows
	}
	if err := m.start(cols, rows); err != nil {
		return nil, fmt.Errorf("restart: %w", err)
	}

	log.Printf("PTY restarted: pid=%d", m.session.Pid())
	return m.outputCh, nil
}

func (m *PTYManager) readLoop(sess *ptySession, ch chan []byte) {
	tmp := make([]byte, readBufSize)
	for {
		n, err := sess.Read(tmp)
		if n > 0 {
			data := make([]byte, n)
			copy(data, tmp[:n])
			m.buf.Write(data)
			ch <- data
		}
		if err != nil {
			break
		}
	}
	close(ch)
}

func (m *PTYManager) waitProcess(sess *ptySession) {
	sess.Wait()
	sess.Shutdown()
	log.Printf("PTY process exited: pid=%d", sess.Pid())
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
	_, err := m.session.Write(data)
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
	return m.session.Resize(cols, rows)
}

// Pid returns the process ID.
func (m *PTYManager) Pid() int {
	return m.session.Pid()
}

// IsRunning returns whether the process is still running.
func (m *PTYManager) IsRunning() bool {
	return m.session.IsRunning()
}

// Close terminates the PTY and process.
func (m *PTYManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	m.session.Kill()
	m.session.Close()
}
