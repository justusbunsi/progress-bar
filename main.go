package main

import (
	"time"

	cmd "github.com/elulcao/progress-bar/cmd"
)

func main() {
	pb := cmd.NewPBar()
	pb.SignalHandler()
	pb.Total = uint16(20)
	pb.DoneStr = "#"
	pb.OngoingStr = "."

	for i := 1; uint16(i) <= pb.Total; i++ {
		pb.RenderPBar(i)
		pb.Println(i)               // Do something here
		time.Sleep(1 * time.Second) // Wait 1 second, for demo purpose
	}

	pb.CleanUp()
}
