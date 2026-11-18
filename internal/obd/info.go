package obd

import (
	"fmt"
	"strings"
)

// Monitor is one emissions readiness monitor.
type Monitor struct {
	Name      string
	Available bool
	Complete  bool
}

// Status is the decoded Mode 01 PID 01 (monitor status since DTCs cleared).
type Status struct {
	MIL      bool
	DTCCount int
	Diesel   bool
	Monitors []Monitor
}

var sparkMonitors = [8]string{"Catalyst", "Heated catalyst", "Evaporative system", "Secondary air", "A/C refrigerant", "O2 sensor", "O2 sensor heater", "EGR / VVT system"}
var dieselMonitors = [8]string{"NMHC catalyst", "NOx / SCR aftertreat.", "", "Boost pressure", "", "Exhaust gas sensor", "PM filter", "EGR / VVT system"}

// DecodeStatus decodes the 4 data bytes of PID 01.
func DecodeStatus(b []byte) Status {
	s := Status{MIL: b[0]&0x80 != 0, DTCCount: int(b[0] & 0x7F), Diesel: b[1]&0x08 != 0}
	common := []string{"Misfire", "Fuel system", "Components"}
	for i, n := range common {
		s.Monitors = append(s.Monitors, Monitor{n, b[1]&(1<<i) != 0, b[1]&(1<<(i+4)) == 0})
	}
	names := sparkMonitors
	if s.Diesel {
		names = dieselMonitors
	}
	for i, n := range names {
		if n == "" {
			continue
		}
		s.Monitors = append(s.Monitors, Monitor{n, b[2]&(1<<i) != 0, b[3]&(1<<i) == 0})
	}
	return s
}

// ReadStatus reads MIL state, DTC count and readiness monitors.
func ReadStatus(q Querier) (Status, error) {
	b, err := mode01(q, 0x01)
	if err != nil {
		return Status{}, err
	}
	if len(b) < 4 {
		return Status{}, fmt.Errorf("PID 01: short reply")
	}
	return DecodeStatus(b), nil
}

// Vehicle holds Mode 09 identification data.
type Vehicle struct {
	VIN     string
	CalID   string
	ECUName map[string]string // by ECU id
	ECUs    []string
}

// mode09 returns per-ECU ASCII payloads for an info type (e.g. 0x02 = VIN).
func mode09(q Querier, info byte) (map[string]string, []string, error) {
	resp, err := q.Query(fmt.Sprintf("09%02X", info))
	if err != nil {
		return nil, nil, err
	}
	can := q.Format().IsCAN()
	out := map[string]string{}
	var order []string
	for _, r := range resp {
		var raw []byte
		if can {
			// 49 II NN <data>
			if len(r.Data) < 3 || r.Data[0] != 0x49 || r.Data[1] != info {
				continue
			}
			raw = r.Data[3:]
		} else {
			// Each frame: 49 II SEQ d d d d
			for _, f := range r.Frames {
				if len(f) >= 3 && f[0] == 0x49 && f[1] == info {
					raw = append(raw, f[3:]...)
				}
			}
		}
		out[r.ECU] = ascii(raw)
		order = append(order, r.ECU)
	}
	return out, order, nil
}

func ascii(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7F {
			sb.WriteByte(c)
		}
	}
	return strings.TrimSpace(sb.String())
}

// ReadVehicle reads VIN, calibration ID and ECU names. Missing items are
// left empty rather than failing the whole read.
func ReadVehicle(q Querier) (Vehicle, error) {
	v := Vehicle{ECUName: map[string]string{}}
	vins, order, err := mode09(q, 0x02)
	if err == nil {
		v.ECUs = order
		for _, e := range order {
			if len(vins[e]) >= 17 {
				v.VIN = vins[e][len(vins[e])-17:]
				break
			}
			if v.VIN == "" {
				v.VIN = vins[e]
			}
		}
	}
	if cal, ord, err := mode09(q, 0x04); err == nil && len(ord) > 0 {
		v.CalID = cal[ord[0]]
	}
	if names, ord, err := mode09(q, 0x0A); err == nil {
		for _, e := range ord {
			v.ECUName[e] = names[e]
			if !contains(v.ECUs, e) {
				v.ECUs = append(v.ECUs, e)
			}
		}
	}
	return v, nil
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// ECULabel gives a friendly name for common CAN response IDs.
func ECULabel(id string) string {
	switch strings.ToUpper(id) {
	case "7E8", "18DAF110":
		return "Engine (" + id + ")"
	case "7E9", "18DAF118":
		return "Transmission (" + id + ")"
	case "7EA":
		return "Hybrid / aux (" + id + ")"
	case "":
		return "ECU"
	}
	return "ECU " + id
}
