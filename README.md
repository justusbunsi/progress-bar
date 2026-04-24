# progress-bar

A terminal progress bar library for Go with a package-manager look. Works on **Linux, macOS, and Windows**.

---

<p align="center">
    <img src="./.assets/demo-01.gif" alt="Demo" width="600" height="400" />
</p>

---

The progress bar renders on the last line of the terminal (stderr), leaving the rest of the window free for program output. It adapts automatically to terminal resize events.

Default characters are `#` (done) and `.` (remaining) but can be changed per bar.

## Single bar

```go
pb := cmd.NewPBar()
pb.Total = 100
pb.SignalHandler()
defer pb.CleanUp()

for i := 1; i <= int(pb.Total); i++ {
    pb.RenderPBar(i)
    pb.Println("step", i)   // prints above the bar without corrupting it
    time.Sleep(100 * time.Millisecond)
}
```

## Concurrent bars (MultiBar)

Multiple bars rendered simultaneously at the bottom — designed for `sync.WaitGroup` workloads. Log lines printed via `Println` scroll above all bars.

```go
mb := cmd.NewMultiBar()
mb.SignalHandler()
defer mb.CleanUp()

var wg sync.WaitGroup
for _, task := range tasks {
    wg.Add(1)
    bar := mb.NewBar(task.Name, uint16(task.Steps))
    go func(b *cmd.Bar) {
        defer wg.Done()
        for i := 1; i <= int(b.Total); i++ {
            b.Set(i)
            mb.Println("finished step", i)
        }
    }(bar)
}
wg.Wait()
```

## Credits

Inspired by and based on [elulcao/progress-bar](https://github.com/elulcao/progress-bar). This fork adds Windows support and concurrent multi-bar rendering.

## Notes

- **Signal handling**: `SignalHandler()` listens for resize events (SIGWINCH on Unix, polling on Windows) and for SIGTERM/SIGINT to clean up the terminal before exit. Call `defer mb.CleanUp()` to ensure the bar line is erased on normal exit.
- **Non-TTY environments**: when stderr is a pipe or CI output, all rendering is skipped automatically and `Println` falls back to standard output.
- **Testing**: unit tests run without a TTY. Use `go run main.go` to visually verify bar output. Integration tests require a PTY: `make test-integration`.
