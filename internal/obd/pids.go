// Package obd implements SAE J1979 OBD-II services on top of an ELM327 link.
package obd

import (
	"errors"
	"fmt"
	"math"

	"lazyobd/internal/elm"
)

// Querier is what the obd package needs from an adapter connection.
type Querier interface {
	Query(cmd string) ([]elm.Response, error)
	Format() elm.Format
}

// PID describes one Mode 01 parameter.
type PID struct {
	Code   byte
	Name   string
	Unit   string
	Bytes  int
	Min    float64 // gauge range
	Max    float64
	WarnLo float64 // NaN = no low warning
	WarnHi float64 // NaN = no high warning
	Prec   int     // decimals to display
	Fast   bool    // poll every cycle (otherwise every few cycles)
	Decode func(b []byte) float64
}

// Warn reports whether v is outside the PID's healthy range.
func (p PID) Warn(v float64) bool {
	return (!math.IsNaN(p.WarnLo) && v < p.WarnLo) || (!math.IsNaN(p.WarnHi) && v > p.WarnHi)
}

// Format renders v with the PID's precision.
func (p PID) Format(v float64) string { return fmt.Sprintf("%.*f", p.Prec, v) }

var nan = math.NaN()

func a(b []byte) float64  { return float64(b[0]) }
func ab(b []byte) float64 { return float64(int(b[0])<<8 | int(b[1])) }
func pct(b []byte) float64 {
	return a(b) * 100 / 255
}
func temp(b []byte) float64 { return a(b) - 40 }
func trim(b []byte) float64 { return (a(b) - 128) * 100 / 128 }

// PIDs is the table of supported decoders, in dashboard order.
var PIDs = []PID{
	{0x0C, "Engine RPM", "rpm", 2, 0, 7000, nan, 6500, 0, true, func(b []byte) float64 { return ab(b) / 4 }},
	{0x0D, "Vehicle speed", "km/h", 1, 0, 220, nan, nan, 0, true, a},
	{0x05, "Coolant temp", "°C", 1, -40, 130, nan, 105, 0, false, temp},
	{0x04, "Engine load", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x11, "Throttle position", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x0B, "Intake manifold", "kPa", 1, 0, 255, nan, nan, 0, true, a},
	{0x10, "MAF air flow", "g/s", 2, 0, 250, nan, nan, 2, true, func(b []byte) float64 { return ab(b) / 100 }},
	{0x0F, "Intake air temp", "°C", 1, -40, 80, nan, 60, 0, false, temp},
	{0x0E, "Timing advance", "°", 1, -64, 64, nan, nan, 1, true, func(b []byte) float64 { return a(b)/2 - 64 }},
	{0x5C, "Oil temp", "°C", 1, -40, 160, nan, 130, 0, false, temp},
	{0x42, "Module voltage", "V", 2, 0, 16, 12.0, 15.0, 2, false, func(b []byte) float64 { return ab(b) / 1000 }},
	{0x2F, "Fuel level", "%", 1, 0, 100, 10, nan, 1, false, pct},
	{0x5E, "Fuel rate", "L/h", 2, 0, 50, nan, nan, 2, true, func(b []byte) float64 { return ab(b) / 20 }},
	{0x06, "Short fuel trim B1", "%", 1, -25, 25, -20, 20, 1, true, trim},
	{0x07, "Long fuel trim B1", "%", 1, -25, 25, -15, 15, 1, false, trim},
	{0x08, "Short fuel trim B2", "%", 1, -25, 25, -20, 20, 1, true, trim},
	{0x09, "Long fuel trim B2", "%", 1, -25, 25, -15, 15, 1, false, trim},
	{0x46, "Ambient air temp", "°C", 1, -40, 60, nan, nan, 0, false, temp},
	{0x33, "Barometric press.", "kPa", 1, 0, 120, nan, nan, 0, false, a},
	{0x0A, "Fuel pressure", "kPa", 1, 0, 765, nan, nan, 0, false, func(b []byte) float64 { return a(b) * 3 }},
	{0x22, "Fuel rail press.", "kPa", 2, 0, 5000, nan, nan, 1, false, func(b []byte) float64 { return ab(b) * 0.079 }},
	{0x23, "Fuel rail gauge", "kPa", 2, 0, 200000, nan, nan, 0, false, func(b []byte) float64 { return ab(b) * 10 }},
	{0x43, "Absolute load", "%", 2, 0, 400, nan, nan, 1, true, func(b []byte) float64 { return ab(b) * 100 / 255 }},
	{0x44, "Cmd. equiv. ratio", "λ", 2, 0, 2, nan, nan, 3, false, func(b []byte) float64 { return ab(b) * 2 / 65536 }},
	{0x45, "Relative throttle", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x47, "Abs. throttle B", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x49, "Accel pedal D", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x4A, "Accel pedal E", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x4C, "Cmd. throttle", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x5A, "Rel. accel pedal", "%", 1, 0, 100, nan, nan, 1, true, pct},
	{0x61, "Demand torque", "%", 1, -125, 130, nan, nan, 0, true, func(b []byte) float64 { return a(b) - 125 }},
	{0x62, "Actual torque", "%", 1, -125, 130, nan, nan, 0, true, func(b []byte) float64 { return a(b) - 125 }},
	{0x63, "Reference torque", "Nm", 2, 0, 1000, nan, nan, 0, false, ab},
	{0x3C, "Catalyst temp B1S1", "°C", 2, -40, 1000, nan, 900, 0, false, func(b []byte) float64 { return ab(b)/10 - 40 }},
	{0x3D, "Catalyst temp B2S1", "°C", 2, -40, 1000, nan, 900, 0, false, func(b []byte) float64 { return ab(b)/10 - 40 }},
	{0x14, "O2 B1S1 voltage", "V", 2, 0, 1.275, nan, nan, 3, true, func(b []byte) float64 { return a(b) / 200 }},
	{0x15, "O2 B1S2 voltage", "V", 2, 0, 1.275, nan, nan, 3, false, func(b []byte) float64 { return a(b) / 200 }},
	{0x2C, "Commanded EGR", "%", 1, 0, 100, nan, nan, 1, false, pct},
	{0x2D, "EGR error", "%", 1, -100, 100, nan, nan, 1, false, trim},
	{0x2E, "Evap purge", "%", 1, 0, 100, nan, nan, 1, false, pct},
	{0x52, "Ethanol fuel", "%", 1, 0, 100, nan, nan, 1, false, pct},
	{0x5B, "Hybrid battery", "%", 1, 0, 100, 15, nan, 1, false, pct},
	{0x1F, "Run time", "s", 2, 0, 3600, nan, nan, 0, false, ab},
	{0x30, "Warm-ups since clear", "", 1, 0, 255, nan, nan, 0, false, a},
	{0x31, "Dist. since clear", "km", 2, 0, 65535, nan, nan, 0, false, ab},
	{0x21, "Dist. with MIL on", "km", 2, 0, 65535, nan, 0, 0, false, ab},
	{0x4D, "Time with MIL on", "min", 2, 0, 65535, nan, 0, 0, false, ab},
	{0x4E, "Time since clear", "min", 2, 0, 65535, nan, nan, 0, false, ab},
}

var pidByCode = func() map[byte]PID {
	m := map[byte]PID{}
	for _, p := range PIDs {
		m[p.Code] = p
	}
	return m
}()

// Lookup returns the decoder for a PID code.
func Lookup(code byte) (PID, bool) {
	p, ok := pidByCode[code]
	return p, ok
}

// ErrNotSupported means no ECU returned data for the request.
var ErrNotSupported = errors.New("not supported by vehicle")

// mode01 returns the data bytes (after 41 PP) from the first ECU that answers.
func mode01(q Querier, pid byte) ([]byte, error) {
	resp, err := q.Query(fmt.Sprintf("01%02X", pid))
	if err != nil {
		if errors.Is(err, elm.ErrNoData) {
			return nil, ErrNotSupported
		}
		return nil, err
	}
	for _, r := range resp {
		if len(r.Data) >= 2 && r.Data[0] == 0x41 && r.Data[1] == pid {
			return r.Data[2:], nil
		}
	}
	return nil, ErrNotSupported
}

// Read queries and decodes a single PID.
func Read(q Querier, p PID) (float64, error) {
	b, err := mode01(q, p.Code)
	if err != nil {
		return 0, err
	}
	if len(b) < p.Bytes {
		return 0, fmt.Errorf("PID %02X: short reply (%d bytes)", p.Code, len(b))
	}
	return p.Decode(b), nil
}

// Supported walks the 0100/0120/... bitmaps of every responding ECU and
// returns the union of supported PID codes.
func Supported(q Querier) (map[byte]bool, error) {
	sup := map[byte]bool{}
	for base := 0; base < 0xE0; base += 0x20 {
		resp, err := q.Query(fmt.Sprintf("01%02X", base))
		if err != nil {
			if base == 0 {
				return nil, err
			}
			break
		}
		more := false
		for _, r := range resp {
			if len(r.Data) < 6 || r.Data[0] != 0x41 || int(r.Data[1]) != base {
				continue
			}
			bits := uint32(r.Data[2])<<24 | uint32(r.Data[3])<<16 | uint32(r.Data[4])<<8 | uint32(r.Data[5])
			for i := 0; i < 32; i++ {
				if bits&(1<<(31-i)) != 0 {
					sup[byte(base+i+1)] = true
				}
			}
			if bits&1 != 0 {
				more = true
			}
		}
		if !more {
			break
		}
	}
	if len(sup) == 0 {
		return nil, ErrNotSupported
	}
	return sup, nil
}

// SupportedPIDs filters the decoder table down to what the vehicle supports.
func SupportedPIDs(sup map[byte]bool) []PID {
	var out []PID
	for _, p := range PIDs {
		if sup[p.Code] {
			out = append(out, p)
		}
	}
	return out
}
