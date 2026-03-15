//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32                               = syscall.NewLazyDLL("kernel32.dll")
	procCreatePseudoConsole                = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole                = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole                 = kernel32.NewProc("ClosePseudoConsole")
	procInitializeProcThreadAttributeList  = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute          = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList      = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateProcessW                     = kernel32.NewProc("CreateProcessW")
)

const (
	_PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE = 0x00020016
	_EXTENDED_STARTUPINFO_PRESENT        = 0x00080000
)

type startupInfoEx struct {
	syscall.StartupInfo
	lpAttributeList *byte
}

type ptySession struct {
	hPC     uintptr        // ConPTY handle
	inW     syscall.Handle // write → process stdin
	outR    syscall.Handle // read ← process stdout
	process syscall.Handle
	pid     uint32
}

func newPTYSession(name string, args []string, cols, rows uint16, extraEnv []string) (*ptySession, error) {
	// Create I/O pipes
	var inR, inW, outR, outW syscall.Handle
	if err := syscall.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("create input pipe: %w", err)
	}
	if err := syscall.CreatePipe(&outR, &outW, nil, 0); err != nil {
		syscall.CloseHandle(inR)
		syscall.CloseHandle(inW)
		return nil, fmt.Errorf("create output pipe: %w", err)
	}

	// COORD: cols in low word, rows in high word
	coord := uintptr(cols) | (uintptr(rows) << 16)
	var hPC uintptr

	hr, _, _ := procCreatePseudoConsole.Call(
		coord,
		uintptr(inR),
		uintptr(outW),
		0,
		uintptr(unsafe.Pointer(&hPC)),
	)
	if hr != 0 {
		syscall.CloseHandle(inR)
		syscall.CloseHandle(inW)
		syscall.CloseHandle(outR)
		syscall.CloseHandle(outW)
		return nil, fmt.Errorf("CreatePseudoConsole: HRESULT 0x%08x", hr)
	}

	// Close pipe ends now owned by ConPTY
	syscall.CloseHandle(inR)
	syscall.CloseHandle(outW)

	// Initialize proc thread attribute list
	var size uintptr
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	attrBuf := make([]byte, size)
	attrList := &attrBuf[0]

	r, _, err := procInitializeProcThreadAttributeList.Call(
		uintptr(unsafe.Pointer(attrList)), 1, 0, uintptr(unsafe.Pointer(&size)),
	)
	if r == 0 {
		procClosePseudoConsole.Call(hPC)
		syscall.CloseHandle(inW)
		syscall.CloseHandle(outR)
		return nil, fmt.Errorf("InitializeProcThreadAttributeList: %v", err)
	}
	defer procDeleteProcThreadAttributeList.Call(uintptr(unsafe.Pointer(attrList)))

	r, _, err = procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attrList)),
		0,
		_PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		hPC,
		unsafe.Sizeof(hPC),
		0,
		0,
	)
	if r == 0 {
		procClosePseudoConsole.Call(hPC)
		syscall.CloseHandle(inW)
		syscall.CloseHandle(outR)
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %v", err)
	}

	// Build command line
	cmdLine := buildCmdLine(name, args)
	cmdLineUTF16, _ := syscall.UTF16PtrFromString(cmdLine)

	// Build environment block with extra env vars
	var envBlock *uint16
	createFlags := uintptr(_EXTENDED_STARTUPINFO_PRESENT)
	if len(extraEnv) > 0 {
		env := os.Environ()
		env = append(env, extraEnv...)
		envBlock = createEnvBlock(env)
		createFlags |= 0x00000400 // CREATE_UNICODE_ENVIRONMENT
	}

	// Working directory (uses current directory of the process)
	var lpDir *uint16
	if wd, err := os.Getwd(); err == nil {
		lpDir, _ = syscall.UTF16PtrFromString(wd)
	}

	// Set up STARTUPINFOEX with ConPTY attribute
	var si startupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.lpAttributeList = attrList

	var pi syscall.ProcessInformation

	r, _, err = procCreateProcessW.Call(
		0,
		uintptr(unsafe.Pointer(cmdLineUTF16)),
		0, 0, 0,
		createFlags,
		uintptr(unsafe.Pointer(envBlock)),
		uintptr(unsafe.Pointer(lpDir)),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if r == 0 {
		procClosePseudoConsole.Call(hPC)
		syscall.CloseHandle(inW)
		syscall.CloseHandle(outR)
		return nil, fmt.Errorf("CreateProcess '%s': %v", cmdLine, err)
	}

	syscall.CloseHandle(pi.Thread)

	return &ptySession{
		hPC:     hPC,
		inW:     inW,
		outR:    outR,
		process: pi.Process,
		pid:     pi.ProcessId,
	}, nil
}

// buildCmdLine builds a Windows command line string with proper argument quoting.
func buildCmdLine(name string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, quoteWinArg(name))
	for _, a := range args {
		parts = append(parts, quoteWinArg(a))
	}
	return strings.Join(parts, " ")
}

// createEnvBlock creates a Windows environment block (double-null-terminated UTF-16).
func createEnvBlock(env []string) *uint16 {
	var b []uint16
	for _, s := range env {
		u := syscall.StringToUTF16(s)
		b = append(b, u...)
	}
	b = append(b, 0)
	return &b[0]
}

// quoteWinArg quotes a single argument for Windows command line if needed.
func quoteWinArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			j := i
			for j < len(s) && s[j] == '\\' {
				j++
			}
			if j < len(s) && s[j] == '"' {
				for k := i; k < j; k++ {
					b.WriteString(`\\`)
				}
				b.WriteString(`\"`)
				i = j
			} else {
				for k := i; k < j; k++ {
					b.WriteByte('\\')
				}
				i = j - 1
			}
		} else if s[i] == '"' {
			b.WriteString(`\"`)
		} else {
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (s *ptySession) Read(p []byte) (int, error) {
	var n uint32
	err := syscall.ReadFile(s.outR, p, &n, nil)
	if err != nil {
		return int(n), err
	}
	return int(n), nil
}

func (s *ptySession) Write(p []byte) (int, error) {
	var n uint32
	err := syscall.WriteFile(s.inW, p, &n, nil)
	if err != nil {
		return int(n), err
	}
	return int(n), nil
}

func (s *ptySession) Resize(cols, rows uint16) error {
	coord := uintptr(cols) | (uintptr(rows) << 16)
	hr, _, _ := procResizePseudoConsole.Call(s.hPC, coord)
	if hr != 0 {
		return fmt.Errorf("ResizePseudoConsole: HRESULT 0x%08x", hr)
	}
	return nil
}

func (s *ptySession) Pid() int {
	return int(s.pid)
}

func (s *ptySession) IsRunning() bool {
	event, _ := syscall.WaitForSingleObject(s.process, 0)
	return event == uint32(syscall.WAIT_TIMEOUT)
}

func (s *ptySession) Kill() {
	_ = syscall.TerminateProcess(s.process, 1)
}

func (s *ptySession) Wait() {
	_, _ = syscall.WaitForSingleObject(s.process, syscall.INFINITE)
}

// Shutdown closes the ConPTY handle and output pipe to unblock readLoop after process exit.
// On Windows, ReadFile on the output pipe blocks until both ConPTY and the pipe are closed.
func (s *ptySession) Shutdown() {
	if s.hPC != 0 {
		procClosePseudoConsole.Call(s.hPC)
		s.hPC = 0
	}
	// Close output pipe to unblock ReadFile in readLoop
	if s.outR != syscall.InvalidHandle {
		syscall.CloseHandle(s.outR)
		s.outR = syscall.InvalidHandle
	}
}

func (s *ptySession) Close() {
	syscall.CloseHandle(s.inW)
	if s.outR != syscall.InvalidHandle {
		syscall.CloseHandle(s.outR)
		s.outR = syscall.InvalidHandle
	}
	if s.hPC != 0 {
		procClosePseudoConsole.Call(s.hPC)
		s.hPC = 0
	}
	syscall.CloseHandle(s.process)
}
