//go:build !windows

package progressbar

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

// MultiBar manages a set of concurrent progress bars rendered at the bottom
// of the terminal. Log lines printed via Println appear above the bars and
// scroll the bar area downward naturally.
//
// Typical usage with sync.WaitGroup:
//
//	mb := cmd.NewMultiBar()
//	mb.SignalHandler()
//	defer mb.CleanUp()
//
//	var wg sync.WaitGroup
//	for _, task := range tasks {
//	    wg.Add(1)
//	    bar := mb.NewBar(task.Name, task.Total)
//	    go func(b *cmd.Bar) {
//	        defer wg.Done()
//	        for i := 1; i <= int(b.Total); i++ {
//	            b.Set(i)
//	        }
//	    }(bar)
//	}
//	wg.Wait()
type MultiBar struct {
	mu   sync.Mutex
	bars []*Bar
	out  io.Writer
	outFd uintptr

	renderedLines int  // number of bar lines currently on screen
	wscol         uint16

	signalWinch chan os.Signal
	signalTerm  chan os.Signal
	closeOnce   sync.Once
	done        chan struct{}

	winSize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
}

// NewMultiBar creates a MultiBar that writes to stderr.
func NewMultiBar() *MultiBar {
	mb := &MultiBar{
		out:         os.Stderr,
		outFd:       os.Stderr.Fd(),
		signalWinch: make(chan os.Signal, 1),
		signalTerm:  make(chan os.Signal, 1),
		done:        make(chan struct{}),
	}
	signal.Notify(mb.signalWinch, syscall.SIGWINCH)
	signal.Notify(mb.signalTerm, syscall.SIGTERM, syscall.SIGINT)
	mb.mu.Lock()
	_ = mb.updateWSizeLocked()
	mb.mu.Unlock()
	return mb
}

// NewBar creates a new Bar with the given label and total step count.
// It can be called before or after SignalHandler.
func (mb *MultiBar) NewBar(label string, total uint16) *Bar {
	b := &Bar{
		mb:         mb,
		Label:      label,
		Total:      total,
		DoneStr:    "#",
		OngoingStr: ".",
	}
	mb.mu.Lock()
	mb.bars = append(mb.bars, b)
	mb.mu.Unlock()
	return b
}

// Close stops signal delivery. Safe to call multiple times.
func (mb *MultiBar) Close() {
	mb.closeOnce.Do(func() {
		signal.Stop(mb.signalWinch)
		signal.Stop(mb.signalTerm)
		close(mb.done)
	})
}

// CleanUp erases all bar lines and stops signal handling.
// Recommended: defer mb.CleanUp().
func (mb *MultiBar) CleanUp() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.Close()
	if ok, _ := mb.isTTYLocked(); !ok {
		return
	}
	n := mb.renderedLines
	if n > 0 {
		fmt.Fprintf(mb.out, "\x1B[%dA", n)
		for i := 0; i < n; i++ {
			fmt.Fprint(mb.out, "\x1B[2K\x1B[1G\n")
		}
		fmt.Fprintf(mb.out, "\x1B[%dA", n)
	}
	mb.renderedLines = 0
}

// SignalHandler starts a goroutine that handles SIGWINCH (redraw) and
// SIGTERM/SIGINT (cleanup + exit).
func (mb *MultiBar) SignalHandler() {
	go func() {
		for {
			select {
			case <-mb.done:
				return
			case <-mb.signalWinch:
				mb.mu.Lock()
				_ = mb.updateWSizeLocked()
				mb.renderAllLocked()
				mb.mu.Unlock()
			case <-mb.signalTerm:
				mb.CleanUp()
				os.Exit(1)
			}
		}
	}()
}

// Println prints a log line above the bar area without corrupting the bars.
// Safe to call from any goroutine.
func (mb *MultiBar) Println(a ...any) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if ok, _ := mb.isTTYLocked(); !ok {
		fmt.Println(a...)
		return
	}

	// Move up to the top of the bar area, overwrite Bar-0's line with the log
	// message, then redraw all bars from the new cursor position (one line
	// below the log). The last bar's trailing \n shifts the bar area down by
	// one line, keeping bars anchored at the bottom.
	n := mb.renderedLines
	if n > 0 {
		fmt.Fprintf(mb.out, "\x1B[%dA", n)
	}
	fmt.Fprint(mb.out, "\x1B[?7l") // DECAWM off: prevent long lines from wrapping into bar rows
	fmt.Fprint(mb.out, "\x1B[2K\x1B[1G")
	fmt.Fprintln(mb.out, a...)
	fmt.Fprint(mb.out, "\x1B[?7h") // DECAWM on
	mb.renderedLines = 0
	mb.renderAllLocked()
}

func (mb *MultiBar) isTTYLocked() (bool, error) {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		mb.outFd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&mb.winSize)),
	)
	if errno != 0 {
		if errno == syscall.ENOTTY || errno == syscall.ENODEV {
			return false, nil
		}
		return false, errno
	}
	return true, nil
}

func (mb *MultiBar) updateWSizeLocked() error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		mb.outFd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&mb.winSize)),
	)
	if errno != 0 {
		return errno
	}
	mb.wscol = mb.winSize.Col
	return nil
}
