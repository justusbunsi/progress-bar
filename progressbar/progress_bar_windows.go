//go:build windows

package progressbar

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// PBar is a terminal progress bar for Windows.
//
// Design goals:
//   - Uses ANSI escape sequences enabled via ENABLE_VIRTUAL_TERMINAL_PROCESSING.
//   - Progress bar writes to stderr; program output goes to stdout.
//   - Polls terminal width every 100 ms (Windows has no SIGWINCH).
type PBar struct {
	Total uint16

	DoneStr    string
	OngoingStr string

	out   io.Writer
	outFd uintptr

	mu sync.Mutex

	lastCount       int
	done            chan struct{}
	lastRenderWidth int
	wscol           uint16

	signalTerm chan os.Signal
	closeOnce  sync.Once
}

// NewPBar creates a new progress bar for Windows.
//
// After calling NewPBar():
//   - set pb.Total
//   - call pb.SignalHandler() once
//   - call pb.RenderPBar(i) during work
//   - defer pb.CleanUp() to clear the line on exit
func NewPBar() *PBar {
	pb := &PBar{
		DoneStr:    "#",
		OngoingStr: ".",
		out:        os.Stderr,
		outFd:      os.Stderr.Fd(),
		signalTerm: make(chan os.Signal, 1),
		done:       make(chan struct{}),
	}

	signal.Notify(pb.signalTerm, os.Interrupt)

	// Enable ANSI/VT escape sequence processing on the console handle (Windows 10+).
	h := windows.Handle(pb.outFd)
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err == nil {
		_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}

	pb.mu.Lock()
	_ = pb.updateWSizeLocked()
	pb.mu.Unlock()

	return pb
}

// Close stops signal delivery and closes the signal channels.
// Safe to call multiple times.
func (pb *PBar) Close() {
	pb.closeOnce.Do(func() {
		signal.Stop(pb.signalTerm)
		close(pb.done)
	})
}

// CleanUp clears the progress bar line and stops signal handling.
//
// Call this at the end of your program (recommended: defer pb.CleanUp()).
func (pb *PBar) CleanUp() {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.Close()

	if ok, _ := pb.isTTYLocked(); !ok {
		return
	}

	fmt.Fprint(pb.out, "\r")
	fmt.Fprint(pb.out, "\x1B[0K")
	pb.lastRenderWidth = 0
}

// SignalHandler starts a goroutine that:
//   - polls terminal width every 100 ms and redraws on resize
//   - handles os.Interrupt (Ctrl+C) for cleanup+exit
//
// Windows has no SIGWINCH, so resize detection requires polling.
func (pb *PBar) SignalHandler() {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-pb.done:
				return
			case <-ticker.C:
				pb.mu.Lock()
				old := pb.wscol
				_ = pb.updateWSizeLocked()
				if pb.wscol != old {
					pb.renderNoLock(pb.lastCount)
				}
				pb.mu.Unlock()
			case <-pb.signalTerm:
				pb.CleanUp()
				os.Exit(1)
			}
		}
	}()
}

// RenderPBar renders the progress bar for the given count.
//
// count is clamped to [0..Total].
func (pb *PBar) RenderPBar(count int) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.lastCount = count
	pb.renderNoLock(count)
}

// Println prints a normal line without corrupting the progress bar.
//
// When attached to a console, it prints to pb.out (same handle as the bar)
// to avoid stdout/stderr interleaving.
// When NOT a console (pipes/CI), it prints to stdout as usual.
func (pb *PBar) Println(a ...any) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if ok, _ := pb.isTTYLocked(); !ok {
		fmt.Println(a...)
		return
	}

	fmt.Fprint(pb.out, "\x1B[?7l") // DECAWM off
	fmt.Fprint(pb.out, "\x1B[2K")  // clear entire line
	fmt.Fprint(pb.out, "\x1B[1G")  // column 1
	fmt.Fprintln(pb.out, a...)
	fmt.Fprint(pb.out, "\x1B[?7h") // DECAWM on

	pb.lastRenderWidth = 0
	pb.renderNoLock(pb.lastCount)
}

// isTTYLocked checks if pb.outFd is attached to a Windows console.
// GetConsoleMode succeeds only for real console handles.
func (pb *PBar) isTTYLocked() (bool, error) {
	var mode uint32
	err := windows.GetConsoleMode(windows.Handle(pb.outFd), &mode)
	return err == nil, nil
}

// updateWSizeLocked refreshes terminal columns using GetConsoleScreenBufferInfo.
// Assumes pb.mu is held.
func (pb *PBar) updateWSizeLocked() error {
	var csbi windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(pb.outFd), &csbi); err != nil {
		return err
	}
	pb.wscol = uint16(csbi.Window.Right - csbi.Window.Left + 1)
	return nil
}
