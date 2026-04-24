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

	renderedLines int
	wscol         uint16

	signalTerm chan os.Signal
	closeOnce  sync.Once
	done       chan struct{}
}

// NewMultiBar creates a MultiBar that writes to stderr.
// Enables ANSI/VT processing on Windows 10+.
func NewMultiBar() *MultiBar {
	mb := &MultiBar{
		out:        os.Stderr,
		outFd:      os.Stderr.Fd(),
		signalTerm: make(chan os.Signal, 1),
		done:       make(chan struct{}),
	}

	signal.Notify(mb.signalTerm, os.Interrupt)

	h := windows.Handle(mb.outFd)
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err == nil {
		_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}

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

// SignalHandler starts a goroutine that polls terminal width every 100 ms
// (Windows has no SIGWINCH) and handles Ctrl+C for cleanup+exit.
func (mb *MultiBar) SignalHandler() {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-mb.done:
				return
			case <-ticker.C:
				mb.mu.Lock()
				old := mb.wscol
				_ = mb.updateWSizeLocked()
				if mb.wscol != old {
					mb.renderAllLocked()
				}
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

	n := mb.renderedLines
	if n > 0 {
		fmt.Fprintf(mb.out, "\x1B[%dA", n)
	}
	fmt.Fprint(mb.out, "\x1B[2K\x1B[1G")
	fmt.Fprint(mb.out, fmt.Sprint(a...))
	fmt.Fprint(mb.out, "\x1B[0K\n")
	mb.renderedLines = 0
	mb.renderAllLocked()
}

func (mb *MultiBar) isTTYLocked() (bool, error) {
	var mode uint32
	err := windows.GetConsoleMode(windows.Handle(mb.outFd), &mode)
	return err == nil, nil
}

func (mb *MultiBar) updateWSizeLocked() error {
	var csbi windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(mb.outFd), &csbi); err != nil {
		return err
	}
	mb.wscol = uint16(csbi.Window.Right - csbi.Window.Left + 1)
	return nil
}
