// lazyobd is a terminal UI for ELM327 USB OBD-II adapters.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"lazyobd/internal/elm"
	"lazyobd/internal/sim"
	"lazyobd/internal/ui"
	"lazyobd/internal/worker"
)

func main() {
	port := flag.String("port", "", "serial device (default: auto-detect /dev/serial/by-id, ttyUSB*, ttyACM*)")
	baud := flag.Int("baud", 0, "baud rate (default: auto-detect 38400, 9600, 115200, …)")
	useSim := flag.Bool("sim", false, "use the built-in ELM327 simulator instead of a real adapter")
	logPath := flag.String("log", "", "record live data to this CSV file from the start")
	probe := flag.Bool("probe", false, "no TUI: connect, print adapter/vehicle responses, exit")
	flag.Parse()

	open := func() (*elm.Conn, error) {
		if *useSim {
			c := elm.NewConn(sim.New())
			c.Info.Port = "sim"
			return c, nil
		}
		return elm.OpenSerial(*port, *baud)
	}

	if *probe {
		os.Exit(runProbe(open))
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := worker.New(open)
	go w.Run(ctx)

	m := ui.New(w, cancel, ui.Options{LogPath: *logPath, Sim: *useSim})
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runProbe(open worker.Opener) int {
	if ports := elm.DetectPorts(); len(ports) > 0 {
		fmt.Println("serial ports found:")
		for _, p := range ports {
			fmt.Println("  ", p)
		}
	} else {
		fmt.Println("no serial ports found under /dev/serial/by-id, /dev/ttyUSB*, /dev/ttyACM*")
	}
	c, err := open()
	if err != nil {
		fmt.Println("open:", err)
		return 1
	}
	defer c.Close()
	fmt.Printf("adapter answered on %s @ %d baud\n", c.Info.Port, c.Info.Baud)
	if err := c.Initialize(); err != nil {
		fmt.Println("initialize:", err)
	} else {
		fmt.Printf("adapter:  %s\nprotocol: %s (%d)\n", c.Info.Version, c.Info.ProtocolName, c.Info.Protocol)
	}
	for _, cmd := range []string{"ATRV", "0100", "0101", "010C", "010D", "0105", "03", "0902"} {
		raw, err := c.Send(cmd, 5*time.Second)
		lines := elm.CleanLines(raw, cmd)
		fmt.Printf("%-5s → %s", cmd, strings.Join(lines, " | "))
		if err != nil {
			fmt.Printf("  (%v)", err)
		}
		fmt.Println()
	}
	return 0
}
