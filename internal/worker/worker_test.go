package worker

import (
	"context"
	"testing"
	"time"

	"lazyobd/internal/elm"
	"lazyobd/internal/sim"
)

func TestWorkerAgainstSim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := New(func() (*elm.Conn, error) {
		s := sim.New()
		s.Latency = time.Millisecond
		return elm.NewConn(s), nil
	})
	go w.Run(ctx)

	var connected, supported, values, dtcs, vehicle, cycles bool
	w.Request(ReqRaw{Cmd: "ATRV"})
	for !(connected && supported && values && dtcs && vehicle && cycles) {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout: connected=%v supported=%v values=%v dtcs=%v vehicle=%v cycles=%v",
				connected, supported, values, dtcs, vehicle, cycles)
		case m := <-w.Out:
			switch m := m.(type) {
			case StatusMsg:
				if m.State == Failed {
					t.Fatalf("failed: %s", m.Detail)
				}
				connected = connected || m.State == Connected
			case SupportedMsg:
				supported = len(m.PIDs) > 10
			case ValueMsg:
				values = values || m.Err == nil
			case DTCMsg:
				dtcs = len(m.Stored) == 2
			case VehicleMsg:
				vehicle = m.Vehicle.VIN == "TMBJJ7NE8L0123456"
			case CycleMsg:
				cycles = true
			}
		}
	}
}
