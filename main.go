package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	cmd "github.com/elulcao/progress-bar/cmd"
)

func main() {
	mb := cmd.NewMultiBar()
	mb.SignalHandler()
	defer mb.CleanUp()

	tasks := []struct {
		label string
		steps int
		delay time.Duration
	}{
		{"Downloading", 120, 60 * time.Millisecond},
		{"Processing ", 75, 90 * time.Millisecond},
		{"Uploading  ", 90, 75 * time.Millisecond},
	}

	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		bar := mb.NewBar(t.label, uint16(t.steps))
		go func(b *cmd.Bar, steps int, delay time.Duration, label string) {
			defer wg.Done()
			for i := 1; i <= steps; i++ {
				b.Set(i)
				if rand.Intn(steps) < 3 || rand.Intn(steps) > 20 {
					mb.Println(fmt.Sprintf("[%s] milestone at step %d", label, i))
				}
				time.Sleep(delay)
			}
		}(bar, t.steps, t.delay, t.label)
	}

	wg.Wait()
}
