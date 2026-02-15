package cmd

import (
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
