package progressbar

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRenderPBar ensures RenderPBar never panics for valid counts.
// Note: output verification is covered by the PTY integration test.
func TestRenderPBar(t *testing.T) {
	pb := NewPBar()
	pb.Total = 100
	pb.DoneStr = "#"
	pb.OngoingStr = "."

	pb.SignalHandler()
	defer pb.CleanUp()

	for count := 0; count <= int(pb.Total); count++ {
		assert.NotPanics(t, func() { pb.RenderPBar(count) }, "RenderPBar should not panic")
	}
}

// nonTTYPBar builds a PBar with an invalid fd so isTTYLocked always returns false.
func nonTTYPBar(buf *bytes.Buffer, total uint16, step int) *PBar {
	return &PBar{
		Total:      total,
		NonTTYStep: step,
		DoneStr:    "#",
		OngoingStr: ".",
		out:        buf,
		outFd:      ^uintptr(0), // invalid fd -> isTTYLocked returns false
		done:       make(chan struct{}),
	}
}

func TestNonTTYPBar_StepBoundaries(t *testing.T) {
	var buf bytes.Buffer
	pb := nonTTYPBar(&buf, 10, 10)

	// 0% (count=0) should never emit.
	pb.RenderPBar(0)
	assert.Empty(t, buf.String())

	// First emission at 10% (count=1 of 10).
	pb.RenderPBar(1)
	assert.Equal(t, "1/10\n", buf.String())

	// No new emission until next 10% boundary.
	pb.RenderPBar(1)
	pb.RenderPBar(1)
	assert.Equal(t, "1/10\n", buf.String())

	// Jumping to 50% should emit once (not once per skipped bucket).
	pb.RenderPBar(5)
	assert.Equal(t, "1/10\n5/10\n", buf.String())

	// 100%.
	pb.RenderPBar(10)
	assert.Equal(t, "1/10\n5/10\n10/10\n", buf.String())

	// Repeated call at 100% must not double-emit.
	pb.RenderPBar(10)
	assert.Equal(t, "1/10\n5/10\n10/10\n", buf.String())
}

func TestNonTTYPBar_Disabled(t *testing.T) {
	var buf bytes.Buffer
	pb := nonTTYPBar(&buf, 10, 0) // step=0 disables output
	for i := 0; i <= 10; i++ {
		pb.RenderPBar(i)
	}
	assert.Empty(t, buf.String())
}

func TestNonTTYPBar_ZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	pb := nonTTYPBar(&buf, 0, 10)
	pb.RenderPBar(0)
	assert.Empty(t, buf.String())
}

func TestNonTTYMultiBar_StepBoundaries(t *testing.T) {
	var buf bytes.Buffer
	mb := &MultiBar{
		out:        &buf,
		outFd:      ^uintptr(0),
		NonTTYStep: 50,
		done:       make(chan struct{}),
	}

	b1 := &Bar{mb: mb, Label: "alpha", Total: 4, DoneStr: "#", OngoingStr: "."}
	b2 := &Bar{mb: mb, Label: "", Total: 2, DoneStr: "#", OngoingStr: "."}
	mb.bars = []*Bar{b1, b2}

	// 0% - no output.
	b1.Set(0)
	assert.Empty(t, buf.String())

	// b1 at 25% - step=50, bucket=0, skip.
	b1.Set(1)
	assert.Empty(t, buf.String())

	// b1 at 50% - bucket=1 > 0, emit.
	b1.Set(2)
	assert.Equal(t, "alpha: 2/4\n", buf.String())

	// b2 at 50% - bucket=1 > 0, emit (no label).
	b2.Set(1)
	assert.Equal(t, "alpha: 2/4\n1/2\n", buf.String())

	// b1 at 100%.
	b1.Set(4)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Equal(t, "alpha: 4/4", lines[len(lines)-1])
}
