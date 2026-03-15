//go:build windows

package main

import (
	"fmt"
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

func newPTYSession(name string, args []string, cols, rows uint16) (*ptySession, error) {
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

	// Set up STARTUPINFOEX with ConPTY attribute
	var si startupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.lpAttributeList = attrList

	var pi syscall.ProcessInformation

	r, _, err = procCreateProcessW.Call(
		0,
		uintptr(unsafe.Pointer(cmdLineUTF16)),
		0, 0, 0,
		_EXTENDED_STARTUPINFO_PRESENT,
		0, 0,
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

// buildCmdLine builds a Windows command line string.
func buildCmdLine(name string, args []string) string {
	line := name
	for _, arg := range args {
		line += " " + arg
	}
	return line
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

// Shutdown closes the ConPTY handle to unblock readLoop after process exit.
// On Windows, ReadFile on the output pipe blocks until ConPTY is closed.
func (s *ptySession) Shutdown() {
	if s.hPC != 0 {
		procClosePseudoConsole.Call(s.hPC)
		s.hPC = 0
	}
}

func (s *ptySession) Close() {
	syscall.CloseHandle(s.inW)
	syscall.CloseHandle(s.outR)
	if s.hPC != 0 {
		procClosePseudoConsole.Call(s.hPC)
		s.hPC = 0
	}
	syscall.CloseHandle(s.process)
}
