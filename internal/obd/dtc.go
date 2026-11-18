package obd

import (
	"errors"
	"fmt"
	"sort"

	"lazyobd/internal/elm"
)

// DTC kinds, named after the service that returns them.
const (
	Stored    = 0x03
	Pending   = 0x07
	Permanent = 0x0A
)

// DTC is one diagnostic trouble code.
type DTC struct {
	Code string // e.g. "P0133"
	ECU  string
	Desc string
}

// DecodeDTC turns the two raw bytes into the SAE string form ("" for 0000).
func DecodeDTC(b1, b2 byte) string {
	if b1 == 0 && b2 == 0 {
		return ""
	}
	sys := "PCBU"[b1>>6]
	return fmt.Sprintf("%c%d%X%02X", sys, (b1>>4)&0x3, b1&0x0F, b2)
}

// ReadDTCs reads codes for service 03 (stored), 07 (pending) or 0A (permanent).
func ReadDTCs(q Querier, service byte) ([]DTC, error) {
	resp, err := q.Query(fmt.Sprintf("%02X", service))
	if err != nil {
		if errors.Is(err, elm.ErrNoData) {
			return nil, nil
		}
		return nil, err
	}
	can := q.Format().IsCAN()
	seen := map[string]bool{}
	var out []DTC
	add := func(ecu string, b []byte) {
		for i := 0; i+1 < len(b); i += 2 {
			c := DecodeDTC(b[i], b[i+1])
			if c == "" || seen[ecu+c] {
				continue
			}
			seen[ecu+c] = true
			out = append(out, DTC{Code: c, ECU: ecu, Desc: Describe(c)})
		}
	}
	for _, r := range resp {
		if len(r.Data) == 0 || r.Data[0] != service+0x40 {
			continue
		}
		if can {
			// 43 NN [b1 b2]*NN
			if len(r.Data) >= 2 {
				add(r.ECU, r.Data[2:])
			}
			continue
		}
		// Legacy: every frame is 4x + 3 code pairs.
		for _, f := range r.Frames {
			if len(f) > 1 && f[0] == service+0x40 {
				add(r.ECU, f[1:])
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

// ClearDTCs sends service 04: clears codes, freeze frames and readiness monitors.
func ClearDTCs(q Querier) error {
	resp, err := q.Query("04")
	if err != nil {
		return err
	}
	for _, r := range resp {
		if len(r.Data) > 0 && r.Data[0] == 0x44 {
			return nil
		}
	}
	return errors.New("ECU did not acknowledge clear (engine running?)")
}
