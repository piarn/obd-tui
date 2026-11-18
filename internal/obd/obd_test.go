package obd_test

import (
	"math"
	"testing"

	"lazyobd/internal/elm"
	"lazyobd/internal/obd"
	"lazyobd/internal/sim"
)

func TestDecoders(t *testing.T) {
	cases := []struct {
		pid  byte
		b    []byte
		want float64
	}{
		{0x0C, []byte{0x1A, 0xF8}, 1726},
		{0x05, []byte{0x7B}, 83},
		{0x0D, []byte{0x3C}, 60},
		{0x10, []byte{0x01, 0x90}, 4},
		{0x42, []byte{0x36, 0x30}, 13.872},
		{0x06, []byte{0x80}, 0},
	}
	for _, c := range cases {
		p, ok := obd.Lookup(c.pid)
		if !ok {
			t.Fatalf("PID %02X missing", c.pid)
		}
		if got := p.Decode(c.b); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("PID %02X: got %v want %v", c.pid, got, c.want)
		}
	}
}

func TestDecodeDTC(t *testing.T) {
	for in, want := range map[[2]byte]string{
		{0x01, 0x33}: "P0133",
		{0x04, 0x20}: "P0420",
		{0x41, 0x23}: "C0123",
		{0x81, 0x00}: "B0100",
		{0xC1, 0x00}: "U0100",
		{0x24, 0x2F}: "P242F",
		{0x00, 0x00}: "",
	} {
		if got := obd.DecodeDTC(in[0], in[1]); got != want {
			t.Errorf("%X: got %q want %q", in, got, want)
		}
	}
	if d := obd.Describe("P0304"); d != "Cylinder 4 misfire detected" {
		t.Errorf("P0304: %q", d)
	}
	if d := obd.Describe("P0360"); d != "Ignition coil J primary/secondary circuit" {
		t.Errorf("P0360: %q", d)
	}
}

func TestDecodeStatus(t *testing.T) {
	s := obd.DecodeStatus([]byte{0x82, 0x07, 0x65, 0x21})
	if !s.MIL || s.DTCCount != 2 || s.Diesel {
		t.Fatalf("%+v", s)
	}
	m := map[string]obd.Monitor{}
	for _, x := range s.Monitors {
		m[x.Name] = x
	}
	if c := m["Catalyst"]; !c.Available || c.Complete {
		t.Errorf("catalyst %+v", c)
	}
	if e := m["Evaporative system"]; !e.Available || !e.Complete {
		t.Errorf("evap %+v", e)
	}
}

func connectSim(t *testing.T) *elm.Conn {
	t.Helper()
	s := sim.New()
	s.Latency = 0
	c := elm.NewConn(s)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSimRoundTrip(t *testing.T) {
	c := connectSim(t)
	if c.Info.Protocol != 6 || c.Info.Version != "ELM327 v1.5" {
		t.Fatalf("info %+v", c.Info)
	}
	sup, err := obd.Supported(c)
	if err != nil {
		t.Fatal(err)
	}
	if !sup[0x0C] || !sup[0x5C] || sup[0x0A] {
		t.Fatalf("supported %v", sup)
	}
	rpm, _ := obd.Lookup(0x0C)
	v, err := obd.Read(c, rpm)
	if err != nil || v < 500 || v > 7000 {
		t.Fatalf("rpm %v %v", v, err)
	}
	dtcs, err := obd.ReadDTCs(c, obd.Stored)
	if err != nil || len(dtcs) != 2 || dtcs[0].Code != "P0133" || dtcs[1].Code != "P0420" {
		t.Fatalf("dtcs %+v %v", dtcs, err)
	}
	veh, err := obd.ReadVehicle(c)
	if err != nil || veh.VIN != "TMBJJ7NE8L0123456" || veh.ECUName["7E9"] != "TCM-TransmisCtrl" {
		t.Fatalf("vehicle %+v %v", veh, err)
	}
	if err := obd.ClearDTCs(c); err != nil {
		t.Fatal(err)
	}
	if dtcs, _ := obd.ReadDTCs(c, obd.Stored); len(dtcs) != 0 {
		t.Fatalf("after clear: %+v", dtcs)
	}
	if v, err := c.Voltage(); err != nil || v < 13 {
		t.Fatalf("voltage %v %v", v, err)
	}
}
