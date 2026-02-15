package cmd

import (
	"fmt"

	"strings"
	"syscall"
	"unsafe"
)

// renderNoLock assumes pb.mu is held.
//
// It draws a single-line progress bar to pb.out (stderr by default).
//
// IMPORTANT:
// - Most "double bar" / "garbage" issues are right-edge auto-wrap.
// - Once the terminal wraps, "\r" may keep you on the *wrapped* line.
// - So we:
//  1. reserve a bigger right margin (cols-3)
//  2. use CHA (CSI 1G) to force column 1
//  3. clear the whole line, draw, then clear to end (EL0)
func (pb *PBar) renderNoLock(count int) {
	ok, _ := pb.isTTYLocked()
	if !ok || pb.Total == 0 {
		return
	}

	// Refresh columns (SIGWINCH can be missed/coalesced).
	_ = pb.updateWSizeLocked()
	cols := int(pb.wscol)
	if cols <= 0 {
		return
	}

	// BIG margin to avoid right-edge wrap (root cause of duplicated bars).
	// On many terminals, printing into the last column can flip the "wrapped" state.
	maxVis := cols - 3
	if maxVis <= 0 {
		return
	}

	// Clamp and compute percent.
	count = clamp(count, 0, int(pb.Total))
	percent := int(count) * 100 / int(pb.Total)

	// Choose a prefix that fits.
	var prefix string
	switch {
	case maxVis <= 9:
		prefix = fmt.Sprintf("[\x1B[33m%3d%%\x1B[0m]", percent)
	case maxVis <= 20:
		prefix = fmt.Sprintf("[\x1B[33m%3d%%\x1B[0m] ", percent)
	default:
		prefix = fmt.Sprintf("Progress: [\x1B[33m%3d%%\x1B[0m] ", percent)
	}

	// If even that prefix won't fit, fall back to the shortest form.
	if visibleLen(prefix) > maxVis {
		prefix = fmt.Sprintf("[\x1B[33m%3d%%\x1B[0m]", percent)
	}

	// Build bar to fit exactly (no truncation needed in normal cases).
	line := prefix
	prefixVis := visibleLen(prefix)

	// Draw bar only if there's room for at least "[]".
	if maxVis > 9 && (maxVis-prefixVis) >= 2 {
		barWidth := maxVis - prefixVis - 2
		if barWidth < 0 {
			barWidth = 0
		}

		doneCount := int(float64(barWidth) * float64(count) / float64(pb.Total))
		doneCount = clamp(doneCount, 0, barWidth)

		done := strings.Repeat(pb.DoneStr, doneCount)
		todo := strings.Repeat(pb.OngoingStr, barWidth-doneCount)

		line = prefix + "[" + done + todo + "]"
	}

	// Absolute guarantee: never exceed maxVis visible columns (prevents wrap).
	line = truncateVisible(line, maxVis)
	vis := visibleLen(line)

	// ---- Draw (terminal-agnostic) ----
	// CHA to column 1, clear entire line, draw, clear to end-of-line.
	fmt.Fprint(pb.out, "\x1B[1G") // CHA: column 1 (more robust than "\r" after wraps)
	fmt.Fprint(pb.out, "\x1B[2K") // EL2: clear entire line
	fmt.Fprint(pb.out, "\x1B[1G") // CHA again (some terminals move cursor on EL2)
	fmt.Fprint(pb.out, line)
	fmt.Fprint(pb.out, "\x1B[0K") // EL0: clear to end-of-line
	// -------------------------------

	pb.lastRenderWidth = vis
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
