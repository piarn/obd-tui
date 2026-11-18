package uds

import (
	"fmt"

	"lazyobd/internal/vehicle"
)

// Addressing says how a module is reached on CAN.
type Addressing int

const (
	// Normal11: 11-bit request ID → 11-bit response ID.
	Normal11 Addressing = iota
	// Extended: BMW style. Request on 0x6F1 with the target address as the
	// first data byte; response on 0x600+target.
	Extended
	// Fixed29: ISO 15765-4 29-bit normal fixed: 18DA<tt>F1 → 18DAF1<tt>.
	Fixed29
)

// Module is one addressable control unit.
type Module struct {
	Name string
	Addr Addressing
	Tx   uint32 // request CAN ID (Normal11)
	Rx   uint32 // response CAN ID (Normal11)
	Tgt  byte   // target address (Extended / Fixed29)
}

// ID is a short human address, e.g. "713", "BMW 12", "29b 10".
func (m Module) ID() string {
	switch m.Addr {
	case Extended:
		return fmt.Sprintf("6F1→%02X", m.Tgt)
	case Fixed29:
		return fmt.Sprintf("18DA%02XF1", m.Tgt)
	}
	return fmt.Sprintf("%03X→%03X", m.Tx, m.Rx)
}

// Key identifies a module across scans.
func (m Module) Key() string { return m.ID() }

func n11(name string, tx, rx uint32) Module {
	return Module{Name: name, Addr: Normal11, Tx: tx, Rx: rx}
}

// obdModules are the ISO 15765-4 physical addresses every CAN car uses
// for emissions-relevant ECUs.
var obdModules = []Module{
	n11("Engine (OBD 7E0)", 0x7E0, 0x7E8),
	n11("Transmission (OBD 7E1)", 0x7E1, 0x7E9),
	n11("OBD ECU 7E2", 0x7E2, 0x7EA),
	n11("OBD ECU 7E3", 0x7E3, 0x7EB),
	n11("OBD ECU 7E4", 0x7E4, 0x7EC),
	n11("OBD ECU 7E5", 0x7E5, 0x7ED),
	n11("OBD ECU 7E6", 0x7E6, 0x7EE),
	n11("OBD ECU 7E7", 0x7E7, 0x7EF),
}

// VAG UDS modules: response ID = request ID + 0x6A (engine and gearbox use
// the OBD pair). Names carry the VCDS/ODIS address word in brackets.
func vag(name string, tx uint32) Module { return n11(name, tx, tx+0x6A) }

var vagModules = []Module{
	n11("Engine [01]", 0x7E0, 0x7E8),
	n11("Transmission [02]", 0x7E1, 0x7E9),
	vag("Gateway [19]", 0x710),
	vag("Steering assist [44]", 0x712),
	vag("ABS / brakes [03]", 0x713),
	vag("Instrument cluster [17]", 0x714),
	vag("Airbag [15]", 0x715),
	vag("Central electronics [09]", 0x70E),
	vag("Climate / HVAC [08]", 0x746),
}

// BMW modules on extended addressing (F/G series fully; E-series D-CAN
// partially, often KWP2000 instead of UDS).
func bmw(name string, tgt byte) Module { return Module{Name: name, Addr: Extended, Tgt: tgt} }

var bmwModules = []Module{
	bmw("DME engine", 0x12),
	bmw("DME 2 engine", 0x13),
	bmw("EGS transmission", 0x18),
	bmw("DSC brakes / stability", 0x29),
	bmw("Airbag (ACSM / MRS)", 0x01),
	bmw("Gateway (ZGW)", 0x10),
	bmw("CAS / FEM body", 0x40),
	bmw("KOMBI instrument cluster", 0x60),
	bmw("IHKA climate", 0x78),
}

// Profile is the list of modules to scan for one make group.
type Profile struct {
	Group   vehicle.Group
	Modules []Module
	// Sweep is the 29-bit discovery range used by deep scan.
	Sweep bool
	Note  string
}

// ProfileFor returns the scan profile for a make group.
func ProfileFor(g vehicle.Group) Profile {
	switch g {
	case vehicle.GroupVAG:
		return Profile{g, vagModules, false, "VAG UDS addresses (request + 0x6A). Deep scan sweeps 29-bit addresses."}
	case vehicle.GroupBMW:
		return Profile{g, append(bmwModules[:len(bmwModules):len(bmwModules)], obdModules[:2]...), false,
			"BMW extended addressing — needs an adapter with ATCEA (ELM327 v1.4+/STN)."}
	case vehicle.GroupHonda:
		return Profile{g, obdModules, true,
			"Honda module addresses vary by model: scan covers OBD addresses; deep scan (D) sweeps 29-bit 18DAxxF1."}
	}
	return Profile{vehicle.GroupGeneric, obdModules, true, "Standard OBD addresses. Deep scan (D) sweeps 29-bit 18DAxxF1."}
}

// SweepModules lists every 29-bit normal-fixed target address.
func SweepModules() []Module {
	var out []Module
	for t := 0; t < 0x100; t++ {
		if t == 0xF1 { // the tester itself
			continue
		}
		out = append(out, Module{Name: fmt.Sprintf("Module %02X (29-bit)", t), Addr: Fixed29, Tgt: byte(t)})
	}
	return out
}
