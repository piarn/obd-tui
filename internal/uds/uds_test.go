package uds

import (
	"testing"

	"lazyobd/internal/elm"
	"lazyobd/internal/obd"
	"lazyobd/internal/sim"
	"lazyobd/internal/vehicle"
)

func TestDecodeUDS(t *testing.T) {
	d := decodeUDS([]byte{0x59, 0x02, 0xFF, 0x40, 0x35, 0x1C, 0x09, 0xC1, 0x00, 0x87, 0x08})
	if len(d) != 2 || d[0].Display() != "C0035-1C" || d[1].Display() != "U0100-87" {
		t.Fatalf("%+v", d)
	}
	if f := d[0].Flags(); len(f) != 2 || f[0] != "active" || f[1] != "confirmed" {
		t.Errorf("flags %v", f)
	}
	if f := d[1].Flags(); f[0] != "intermittent" {
		t.Errorf("flags %v", f)
	}
}

func connect(t *testing.T) *elm.Conn {
	t.Helper()
	s := sim.New()
	s.Latency = 0
	c := elm.NewConn(s)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestScanVAGAgainstSim(t *testing.T) {
	c := connect(t)
	sc, err := NewScanner(c)
	if err != nil {
		t.Fatal(err)
	}
	res := map[string]Result{}
	for _, m := range ProfileFor(vehicle.GroupVAG).Modules {
		r := sc.Scan(m, true)
		if r.Err != nil {
			t.Fatalf("%s: %v", m.Name, r.Err)
		}
		res[m.Name] = r
	}
	eng := res["Engine [01]"]
	if eng.State != OK || len(eng.DTCs) != 2 || eng.DTCs[0].Code != "P0133" || eng.PartNo != "04L906026FN" {
		t.Errorf("engine %+v", eng)
	}
	abs := res["ABS / brakes [03]"]
	if abs.State != OK || len(abs.DTCs) != 2 || abs.SysName != "ESC" {
		t.Errorf("abs %+v", abs)
	}
	if r := res["Central electronics [09]"]; r.State != Refused || r.NRC != 0x22 {
		t.Errorf("bcm %+v", r)
	}
	if r := res["Steering assist [44]"]; r.State != Absent {
		t.Errorf("steering %+v", r)
	}
	if r := res["Airbag [15]"]; r.State != OK || len(r.DTCs) != 0 {
		t.Errorf("airbag %+v", r)
	}

	// ABS only clears in the extended session: exercises the fallback.
	var absMod Module
	for _, m := range vagModules {
		if m.Tx == 0x713 {
			absMod = m
		}
	}
	if err := sc.Clear(absMod, false); err != nil {
		t.Fatal(err)
	}
	if r := sc.Scan(absMod, false); len(r.DTCs) != 0 {
		t.Fatalf("after clear %+v", r.DTCs)
	}

	// 29-bit sweep finds the engine at 0x10.
	r := sc.Scan(Module{Name: "29b 10", Addr: Fixed29, Tgt: 0x10}, false)
	if r.State != OK || len(r.DTCs) != 2 {
		t.Fatalf("29-bit %+v", r)
	}
	if r := sc.Scan(Module{Name: "29b 33", Addr: Fixed29, Tgt: 0x33}, false); r.State != Absent {
		t.Fatalf("29-bit absent %+v", r)
	}

	// Back to normal OBD afterwards.
	if err := sc.Restore(); err != nil {
		t.Fatal(err)
	}
	rpm, _ := obd.Lookup(0x0C)
	if _, err := obd.Read(c, rpm); err != nil {
		t.Fatalf("OBD after restore: %v", err)
	}
}
