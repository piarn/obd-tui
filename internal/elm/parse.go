// Package elm talks to ELM327-compatible OBD-II adapters and parses their
// textual responses into per-ECU byte payloads.
package elm

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Error is an error string reported by the adapter itself (e.g. "NO DATA").
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrNoData          Error = "NO DATA"
	ErrUnableToConnect Error = "UNABLE TO CONNECT"
	ErrCAN             Error = "CAN ERROR"
	ErrBus             Error = "BUS ERROR"
	ErrBusInit         Error = "BUS INIT: ERROR"
	ErrBufferFull      Error = "BUFFER FULL"
	ErrStopped         Error = "STOPPED"
	ErrUnknownCmd      Error = "?"
	ErrGeneric         Error = "ERROR"
)

var adapterErrors = []Error{
	ErrNoData, ErrUnableToConnect, ErrCAN, ErrBusInit, ErrBus,
	ErrBufferFull, ErrStopped, ErrUnknownCmd,
	"FB ERROR", "DATA ERROR", "LV RESET", "ACT ALERT", "<RX ERROR",
}

// Response is the reassembled answer of one ECU.
type Response struct {
	ECU    string   // header / sender id, "" when headers are off
	Data   []byte   // full payload, service byte first (CAN: ISO-TP reassembled)
	Frames [][]byte // individual frame payloads in arrival order
}

// Format describes how the adapter is currently formatting output.
type Format struct {
	Headers  bool
	Protocol int  // ELM protocol number: 1..9, A..C → 10..12, 0 = unknown
	ExtAddr  bool // ISO 15765 extended addressing (BMW): frames carry a leading address byte
}

// IsCAN reports whether the active protocol is ISO 15765 (CAN).
func (f Format) IsCAN() bool { return f.Protocol >= 6 }

// CleanLines splits raw adapter output into meaningful lines, dropping the
// prompt, echoes of cmd, and progress chatter like "SEARCHING...".
func CleanLines(raw, cmd string) []string {
	raw = strings.ReplaceAll(raw, "\n", "\r")
	var out []string
	for _, l := range strings.Split(raw, "\r") {
		l = strings.TrimSpace(strings.TrimRight(l, ">"))
		l = strings.TrimSpace(l)
		u := strings.ToUpper(l)
		switch {
		case l == "":
		case cmd != "" && strings.EqualFold(strings.ReplaceAll(u, " ", ""), strings.ReplaceAll(strings.ToUpper(cmd), " ", "")):
		case strings.HasPrefix(u, "SEARCHING"):
		case u == "BUS INIT: ...OK" || u == "BUS INIT: OK" || u == "BUS INIT...":
		default:
			out = append(out, l)
		}
	}
	return out
}

// lineError returns the adapter error a line represents, if any.
func lineError(l string) (Error, bool) {
	u := strings.ToUpper(strings.TrimSpace(l))
	for _, e := range adapterErrors {
		if u == string(e) || strings.HasPrefix(u, string(e)) {
			return e, true
		}
	}
	if u == string(ErrGeneric) {
		return ErrGeneric, true
	}
	return "", false
}

func decodeHexLine(l string) ([]byte, bool) {
	s := strings.ReplaceAll(l, " ", "")
	if len(s)%2 == 1 {
		return nil, false
	}
	b, err := hex.DecodeString(s)
	return b, err == nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789ABCDEFabcdef", r) {
			return false
		}
	}
	return s != ""
}

// Parse converts cleaned adapter lines into per-ECU responses.
// Negative responses (7F sid nrc) are kept; callers match on the
// positive service byte they expect.
func Parse(lines []string, f Format) ([]Response, error) {
	var data []string
	var firstErr error
	for _, l := range lines {
		if e, ok := lineError(l); ok {
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		data = append(data, strings.ToUpper(strings.ReplaceAll(l, " ", "")))
	}
	if len(data) == 0 {
		if firstErr == nil {
			firstErr = ErrNoData
		}
		return nil, firstErr
	}

	var resp []Response
	var err error
	switch {
	case hasSegments(data):
		resp, err = parseSegmented(data)
	case f.Headers && f.IsCAN():
		resp, err = parseCANHeaders(data, f.ExtAddr)
	case f.Headers:
		resp, err = parseLegacyHeaders(data, f.Protocol)
	default:
		resp, err = parsePlain(data)
	}
	if err != nil {
		return nil, err
	}
	out := resp[:0]
	for _, r := range resp {
		if len(r.Data) > 0 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoData
	}
	return out, nil
}

// hasSegments detects the headers-off CAN multi-frame format:
//
//	014
//	0: 49 02 01 31 47 31
//	1: ...
func hasSegments(lines []string) bool {
	for _, l := range lines {
		if len(l) > 2 && l[1] == ':' && isHex(l[:1]) {
			return true
		}
	}
	return false
}

func parseSegmented(lines []string) ([]Response, error) {
	total := -1
	var r Response
	for _, l := range lines {
		if i := strings.IndexByte(l, ':'); i > 0 && i <= 2 {
			b, ok := decodeHexLine(l[i+1:])
			if !ok {
				return nil, fmt.Errorf("bad segment %q", l)
			}
			r.Frames = append(r.Frames, b)
			r.Data = append(r.Data, b...)
			continue
		}
		if len(l) == 3 && isHex(l) {
			var n int
			fmt.Sscanf(l, "%x", &n)
			total = n
			continue
		}
		b, ok := decodeHexLine(l)
		if !ok {
			return nil, fmt.Errorf("unexpected line %q", l)
		}
		r.Frames = append(r.Frames, b)
		r.Data = append(r.Data, b...)
	}
	if total >= 0 && total < len(r.Data) {
		r.Data = r.Data[:total]
	}
	return []Response{r}, nil
}

// parseCANHeaders handles CAN output with headers on. The ELM still shows the
// ISO-TP PCI byte in this mode, so frames are reassembled here.
// 11-bit headers are 3 hex chars (odd total length), 29-bit are 8.
func parseCANHeaders(lines []string, ext bool) ([]Response, error) {
	type asm struct {
		r     Response
		total int
		open  bool
	}
	byECU := map[string]*asm{}
	var done []Response
	var order []string

	for _, l := range lines {
		hl := 8
		if len(l)%2 == 1 {
			hl = 3
		}
		if len(l) < hl+2 {
			continue
		}
		hdr := l[:hl]
		b, ok := decodeHexLine(l[hl:])
		if !ok || len(b) == 0 {
			return nil, fmt.Errorf("bad CAN frame %q", l)
		}
		// Extended addressing: first byte is the target address (e.g. F1
		// for the tester), not a PCI byte. Strip it if the adapter shows it.
		if ext && len(b) > 1 && b[0]>>4 > 3 {
			b = b[1:]
		}
		a := byECU[hdr]
		switch b[0] >> 4 {
		case 0: // single frame
			n := int(b[0] & 0x0F)
			if n > len(b)-1 {
				n = len(b) - 1
			}
			p := append([]byte(nil), b[1:1+n]...)
			done = append(done, Response{ECU: hdr, Data: p, Frames: [][]byte{p}})
		case 1: // first frame
			if len(b) < 2 {
				continue
			}
			total := int(b[0]&0x0F)<<8 | int(b[1])
			p := append([]byte(nil), b[2:]...)
			a = &asm{r: Response{ECU: hdr, Data: p, Frames: [][]byte{p}}, total: total, open: true}
			if _, seen := byECU[hdr]; !seen {
				order = append(order, hdr)
			}
			byECU[hdr] = a
		case 2: // consecutive frame
			if a == nil || !a.open {
				continue
			}
			p := append([]byte(nil), b[1:]...)
			a.r.Frames = append(a.r.Frames, p)
			a.r.Data = append(a.r.Data, p...)
		default: // flow control etc.
		}
	}
	for _, h := range order {
		a := byECU[h]
		if a.total < len(a.r.Data) {
			a.r.Data = a.r.Data[:a.total]
		}
		done = append(done, a.r)
	}
	sortByECU(done)
	return done, nil
}

// parseLegacyHeaders handles J1850 / ISO 9141 / KWP output with headers on:
// header bytes, payload, trailing checksum byte.
func parseLegacyHeaders(lines []string, proto int) ([]Response, error) {
	byECU := map[string]*Response{}
	var order []string
	for _, l := range lines {
		b, ok := decodeHexLine(l)
		if !ok {
			return nil, fmt.Errorf("bad frame %q", l)
		}
		hl := 3
		if (proto == 4 || proto == 5) && len(b) > 0 && b[0]&0x3F == 0 {
			hl = 4 // KWP with separate length byte
		}
		if len(b) < hl+2 {
			continue
		}
		ecu := fmt.Sprintf("%02X", b[2])
		p := append([]byte(nil), b[hl:len(b)-1]...)
		r := byECU[ecu]
		if r == nil {
			r = &Response{ECU: ecu}
			byECU[ecu] = r
			order = append(order, ecu)
		}
		r.Frames = append(r.Frames, p)
		r.Data = append(r.Data, p...)
	}
	var out []Response
	for _, e := range order {
		out = append(out, *byECU[e])
	}
	sortByECU(out)
	return out, nil
}

func parsePlain(lines []string) ([]Response, error) {
	var r Response
	for _, l := range lines {
		b, ok := decodeHexLine(l)
		if !ok {
			return nil, fmt.Errorf("unexpected line %q", l)
		}
		r.Frames = append(r.Frames, b)
		r.Data = append(r.Data, b...)
	}
	return []Response{r}, nil
}

func sortByECU(r []Response) {
	sort.SliceStable(r, func(i, j int) bool { return r[i].ECU < r[j].ECU })
}
