package progressbar

import (
	"fmt"
	"strings"
)

// Bar is a single named progress bar owned by a MultiBar.
// Obtain one via MultiBar.NewBar(); call Set or Inc from any goroutine.
type Bar struct {
	mb         *MultiBar
	Label      string
	Total      uint16
	DoneStr    string
	OngoingStr string
	count      int
}

// Set updates the bar to count and triggers a redraw of all bars.
func (b *Bar) Set(count int) {
	b.mb.mu.Lock()
	b.count = clamp(count, 0, int(b.Total))
	b.mb.renderAllLocked()
	b.mb.mu.Unlock()
}

// Inc increments the bar by 1 and triggers a redraw of all bars.
func (b *Bar) Inc() {
	b.mb.mu.Lock()
	b.count = clamp(b.count+1, 0, int(b.Total))
	b.mb.renderAllLocked()
	b.mb.mu.Unlock()
}

// renderAllLocked redraws every bar in mb.bars.
// If bars have been drawn before, it first moves the cursor up to the top of
// the bar area so each bar line is overwritten in place.
// Assumes mb.mu is held.
func (mb *MultiBar) renderAllLocked() {
	ok, _ := mb.isTTYLocked()
	if !ok {
		return
	}

	_ = mb.updateWSizeLocked()
	cols := int(mb.wscol)
	if cols <= 0 {
		return
	}

	n := len(mb.bars)
	if n == 0 {
		return
	}

	// Move cursor back to the top of the bar area.
	if mb.renderedLines > 0 {
		fmt.Fprintf(mb.out, "\x1B[%dA", mb.renderedLines)
	}

	for _, b := range mb.bars {
		mb.renderBarLineLocked(b, cols)
		fmt.Fprint(mb.out, "\n")
	}

	mb.renderedLines = n
}

// renderBarLineLocked draws one bar line for b onto mb.out.
// Format: "Label: [  42%] [###.........]"
// Assumes mb.mu is held.
func (mb *MultiBar) renderBarLineLocked(b *Bar, cols int) {
	// Clear current line and move to column 1.
	fmt.Fprint(mb.out, "\x1B[2K\x1B[1G")

	maxVis := cols - 3
	if maxVis <= 0 {
		return
	}

	var percent int
	if b.Total > 0 {
		percent = clamp(b.count, 0, int(b.Total)) * 100 / int(b.Total)
	}

	// Build the prefix, falling back to shorter forms if the label is wide.
	var prefix string
	if b.Label != "" {
		prefix = fmt.Sprintf("%s: [\x1B[33m%3d%%\x1B[0m] ", b.Label, percent)
	} else {
		prefix = fmt.Sprintf("[\x1B[33m%3d%%\x1B[0m] ", percent)
	}
	if visibleLen(prefix) > maxVis {
		prefix = fmt.Sprintf("[\x1B[33m%3d%%\x1B[0m] ", percent)
	}
	if visibleLen(prefix) > maxVis {
		prefix = fmt.Sprintf("[\x1B[33m%3d%%\x1B[0m]", percent)
	}

	prefixVis := visibleLen(prefix)
	line := prefix

	// Draw the [###...] portion when there is room for at least "[x]".
	if maxVis > prefixVis+2 {
		barWidth := maxVis - prefixVis - 2
		var doneCount int
		if b.Total > 0 {
			doneCount = clamp(int(float64(barWidth)*float64(b.count)/float64(b.Total)), 0, barWidth)
		}
		line = prefix + "[" +
			strings.Repeat(b.DoneStr, doneCount) +
			strings.Repeat(b.OngoingStr, barWidth-doneCount) +
			"]"
	}

	line = truncateVisible(line, maxVis)
	fmt.Fprint(mb.out, line)
	fmt.Fprint(mb.out, "\x1B[0K") // clear any remnant to end-of-line
}
