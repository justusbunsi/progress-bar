//go:build !windows

package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

// PBar is a simple terminal progress bar for non-Windows platforms.
//
// Design goals:
//   - Terminal-agnostic: uses only widely supported ANSI sequences:
//   - "\r" (carriage return) to go to start of line
//   - "\x1B[0K" (EL0) to clear from cursor to end of line (better on "grow wider")
//   - Safe with "real program" output:
//   - The progress bar writes to stderr (by default)
//   - Your program can print to stdout (numbers/logs) without corrupting the bar
//   - Resizes safely:
//   - Horizontal resize: recompute width from TTY columns and redraw
//   - Vertical resize: we do NOT pin to the bottom row or use scroll regions;
//     instead we ensure we never leave leftover characters by erasing the tail
//     from the previous render (lastRenderWidth).
type PBar struct {
	Total uint16 // Total number of iterations to sum 100%

	DoneStr    string // progress bar "done" character(s)
	OngoingStr string // progress bar "remaining" character(s)

	// out is where the progress bar is rendered (default: stderr).
	// Keeping the bar on stderr avoids mixing it with "real program" stdout output.
	out   io.Writer
	outFd uintptr // fd used for TTY checks + winsize ioctl (matches out)

	mu sync.Mutex // serialize all terminal IO and state changes

	lastCount int // last RenderPBar() count; used to redraw on SIGWINCH

	done chan struct{}

	// lastRenderWidth tracks how many visible columns were printed by the last render.
	// When the terminal is resized, terminals can reflow or leave remnants.
	// We erase any "tail" from a previous longer render to prevent leftover garbage.
	lastRenderWidth int

	wscol uint16 // cached terminal columns (from ioctl)

	signalWinch chan os.Signal // SIGWINCH (window resize)
	signalTerm  chan os.Signal // SIGTERM/SIGINT (cleanup)
	closeOnce   sync.Once      // close channels only once

	// winSize is populated by ioctl(TIOCGWINSZ).
	winSize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
}

// NewPBar creates a new progress bar instance.
//
// After calling NewPBar():
//   - set pb.Total
//   - call pb.SignalHandler() once
//   - call pb.RenderPBar(i) during work
//   - defer pb.CleanUp() to clear the line on exit
func NewPBar() *PBar {
	pb := &PBar{
		DoneStr:     "#",
		OngoingStr:  ".",
		out:         os.Stderr,      // progress bar defaults to stderr
		outFd:       os.Stderr.Fd(), // and we query tty size on stderr
		signalWinch: make(chan os.Signal, 1),
		signalTerm:  make(chan os.Signal, 1),
		done:        make(chan struct{}),
	}

	// Listen for window resize and termination signals.
	signal.Notify(pb.signalWinch, syscall.SIGWINCH)
	signal.Notify(pb.signalTerm, syscall.SIGTERM, syscall.SIGINT)

	// Best-effort initial size read (no panic if not a tty).
	pb.mu.Lock()
	_ = pb.updateWSizeLocked()
	pb.mu.Unlock()

	return pb
}

// Close stops signal delivery and closes the signal channels.
// Safe to call multiple times.
func (pb *PBar) Close() {
	pb.closeOnce.Do(func() {
		signal.Stop(pb.signalWinch)
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

	// If we're not attached to a TTY, do nothing (e.g., running in CI/pipeline).
	if ok, _ := pb.isTTYLocked(); !ok {
		return
	}

	// Clear from cursor to end-of-line (EL0). We first CR to be sure we're at col 1.
	fmt.Fprint(pb.out, "\r")
	fmt.Fprint(pb.out, "\x1B[0K")
	pb.lastRenderWidth = 0
}

// SignalHandler starts a goroutine that listens to SIGWINCH/SIGTERM.
//
// - SIGWINCH: recompute terminal columns and redraw the bar.
// - SIGTERM/SIGINT: cleanup and exit(1).
func (pb *PBar) SignalHandler() {
	go func() {
		for {
			select {
			case <-pb.done:
				return

			case <-pb.signalWinch:
				pb.mu.Lock()
				_ = pb.updateWSizeLocked()
				pb.renderNoLock(pb.lastCount)
				pb.mu.Unlock()

			case <-pb.signalTerm:
				pb.CleanUp()
				os.Exit(1)
			}
		}
	}()
}

// isTTYLocked checks if pb.outFd is attached to a terminal (TTY).
// It uses ioctl(TIOCGWINSZ); ENOTTY/ENODEV mean "not a terminal".
func (pb *PBar) isTTYLocked() (bool, error) {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		pb.outFd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&pb.winSize)),
	)
	if errno != 0 {
		if errno == syscall.ENOTTY || errno == syscall.ENODEV {
			return false, nil
		}
		return false, errno
	}
	return true, nil
}

// updateWSizeLocked refreshes terminal columns using ioctl(TIOCGWINSZ).
// Assumes pb.mu is held.
func (pb *PBar) updateWSizeLocked() error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		pb.outFd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&pb.winSize)),
	)
	if errno != 0 {
		return errno
	}
	pb.wscol = pb.winSize.Col
	return nil
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
// When attached to a TTY, we print the message to pb.out (same fd as the bar)
// to avoid stdout/stderr interleaving that can glue/duplicate progress renders.
// When NOT a TTY (pipes/CI), we print to stdout as usual.
func (pb *PBar) Println(a ...any) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if ok, _ := pb.isTTYLocked(); !ok {
		fmt.Println(a...)
		return
	}

	// Disable auto-wrap while manipulating the bar line.
	fmt.Fprint(pb.out, "\x1B[?7l") // DECAWM off
	fmt.Fprint(pb.out, "\x1B[2K")  // clear entire line
	fmt.Fprint(pb.out, "\x1B[1G")  // column 1

	// Print the message to the SAME stream as the bar (pb.out) + newline.
	// This avoids cross-FD interleaving.
	fmt.Fprintln(pb.out, a...)

	// Re-enable wrap before drawing bar again.
	fmt.Fprint(pb.out, "\x1B[?7h") // DECAWM on

	// The bar line is gone (we printed a newline), reset tail tracker.
	pb.lastRenderWidth = 0

	// Redraw the bar after printing the line.
	pb.renderNoLock(pb.lastCount)
}
