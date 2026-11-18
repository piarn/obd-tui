// Package uds reads and clears trouble codes in every control module using
// UDS (ISO 14229) over CAN, falling back to KWP2000-on-CAN where needed.
package uds

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"lazyobd/internal/elm"
	"lazyobd/internal/obd"
)

// Status bits of a UDS DTC (ISO 14229 DTCStatusMask).
const (
	StTestFailed      = 0x01
	StFailedThisCycle = 0x02
	StPending         = 0x04
	StConfirmed       = 0x08
	StWarningLamp     = 0x80
)

// DTC is one module trouble code.
type DTC struct {
	Code   string // "P0133" (UDS) or "2A82" (KWP raw)
	FTB    byte   // failure type byte (UDS only)
	Status byte
	KWP    bool
	Desc   string
}

// Display renders the code with its failure type, e.g. "C0035-1C".
func (d DTC) Display() string {
	if d.KWP || d.FTB == 0 {
		return d.Code
	}
	return fmt.Sprintf("%s-%02X", d.Code, d.FTB)
}

// Flags is a compact description of the status byte.
func (d DTC) Flags() []string {
	var f []string
	if d.Status&StTestFailed != 0 {
		f = append(f, "active")
	} else if d.Status&StConfirmed != 0 {
		f = append(f, "intermittent")
	}
	if d.Status&StConfirmed != 0 {
		f = append(f, "confirmed")
	}
	if d.Status&StPending != 0 {
		f = append(f, "pending")
	}
	if d.Status&StWarningLamp != 0 {
		f = append(f, "warning lamp")
	}
	return f
}

// State of a module after a scan.
type State int

const (
	Absent  State = iota // no answer
	OK                   // answered, codes read
	Refused              // answered with a negative response
)

// Result is the scan outcome for one module.
type Result struct {
	Module   Module
	State    State
	DTCs     []DTC
	NRC      byte   // when Refused
	KWP      bool   // codes came from KWP2000 service 18
	PartNo   string // DID F187
	SysName  string // DID F197
	Err      error
	Duration time.Duration
}

// NRCText names a UDS negative response code.
func NRCText(n byte) string {
	if s, ok := nrcNames[n]; ok {
		return s
	}
	return fmt.Sprintf("NRC %02X", n)
}

var nrcNames = map[byte]string{
	0x10: "general reject", 0x11: "service not supported", 0x12: "sub-function not supported",
	0x13: "incorrect message length", 0x14: "response too long", 0x21: "busy, repeat request",
	0x22: "conditions not correct", 0x24: "request sequence error", 0x31: "request out of range",
	0x33: "security access denied", 0x35: "invalid key", 0x72: "general programming failure",
	0x78: "response pending", 0x7E: "sub-function not supported in active session",
	0x7F: "service not supported in active session",
}

// FTBText names common failure type bytes (SAE J2012 / ISO 15031-6).
func FTBText(b byte) string {
	if s, ok := ftbNames[b]; ok {
		return s
	}
	return ""
}

var ftbNames = map[byte]string{
	0x01: "general electrical failure", 0x11: "circuit short to ground", 0x12: "circuit short to battery",
	0x13: "circuit open", 0x14: "short to ground or open", 0x15: "short to battery or open",
	0x16: "circuit voltage below threshold", 0x17: "circuit voltage above threshold",
	0x1C: "circuit voltage out of range", 0x29: "signal invalid", 0x31: "no signal",
	0x49: "internal electronic failure", 0x4B: "over temperature", 0x54: "missing calibration",
	0x55: "not configured", 0x64: "signal plausibility failure", 0x81: "invalid serial data received",
	0x86: "signal invalid", 0x87: "missing message", 0x88: "bus off", 0x92: "performance or incorrect operation",
	0x96: "component internal failure",
}

// ErrNotCAN means the vehicle bus isn't CAN, so module scans can't run.
var ErrNotCAN = errors.New("module scan needs a CAN vehicle (ISO 15765)")

// ErrAdapter means the adapter lacks the addressing commands needed.
type ErrAdapter struct{ Cmd string }

func (e ErrAdapter) Error() string {
	return fmt.Sprintf("adapter does not support %s — module scans need a genuine ELM327 v1.4+ or an STN-based adapter (e.g. OBDLink)", e.Cmd)
}

// Scanner runs module requests on a connection.
type Scanner struct {
	C       *elm.Conn
	Timeout time.Duration // per-request wait
	cur     string        // last addressing setup, to skip redundant AT commands
	extOn   bool
	// NoFlowControl is set when the adapter rejects ATFCSH: multi-frame
	// answers (more than one code) from non-OBD modules may then be lost.
	NoFlowControl bool
}

// NewScanner prepares the adapter for module addressing.
func NewScanner(c *elm.Conn) (*Scanner, error) {
	f := c.Format()
	if !f.IsCAN() {
		return nil, ErrNotCAN
	}
	s := &Scanner{C: c, Timeout: 1500 * time.Millisecond}
	// Probe the commands we rely on so clones fail with a clear message.
	for _, cmd := range []string{"ATCRA 7E8", "ATFCSM0", "ATCRA"} {
		if err := c.ATOK(cmd); err != nil {
			return nil, ErrAdapter{strings.Fields(cmd)[0]}
		}
	}
	return s, nil
}

// SetFastTimeout lowers the adapter's response wait for sweeps (~100 ms).
func (s *Scanner) SetFastTimeout() error {
	s.Timeout = 600 * time.Millisecond
	_ = s.C.ATOK("ATAT0")
	return s.C.ATOK("ATST19")
}

// address configures header, receive filter and flow control for m.
func (s *Scanner) address(m Module) error {
	if s.cur == m.Key() {
		return nil
	}
	var cmds, soft []string
	ext := false
	switch m.Addr {
	case Normal11:
		cmds = s.protocol11()
		tx := fmt.Sprintf("%03X", m.Tx)
		cmds = append(cmds, "ATSH "+tx, fmt.Sprintf("ATCRA %03X", m.Rx))
		soft = []string{"ATFCSH " + tx, "ATFCSD 300000", "ATFCSM1"}
	case Extended:
		ext = true
		cmds = s.protocol11()
		cmds = append(cmds, "ATSH 6F1", fmt.Sprintf("ATCEA %02X", m.Tgt), fmt.Sprintf("ATCRA %03X", 0x600+int(m.Tgt)))
		soft = []string{"ATFCSH 6F1", "ATFCSD 300000", "ATFCSM1"}
	case Fixed29:
		cmds = s.protocol29()
		cmds = append(cmds, "ATCP 18", fmt.Sprintf("ATSH DA%02XF1", m.Tgt), fmt.Sprintf("ATCRA 18DAF1%02X", m.Tgt))
		soft = []string{"ATFCSM0"} // the ELM handles 29-bit flow control itself
	}
	if s.extOn && !ext {
		cmds = append([]string{"ATCEA"}, cmds...)
	}
	for _, c := range cmds {
		if err := s.C.ATOK(c); err != nil {
			s.cur = ""
			return ErrAdapter{strings.Fields(c)[0]}
		}
	}
	for _, c := range soft {
		if err := s.C.ATOK(c); err != nil {
			s.NoFlowControl = true
		}
	}
	s.extOn = ext
	s.C.SetExtAddr(ext)
	s.cur = m.Key()
	return nil
}

// protocol11/29 switch between the 11-bit and 29-bit variant of the
// car's CAN speed (6↔7 at 500k, 8↔9 at 250k).
func (s *Scanner) protocol11() []string {
	p := s.C.Format().Protocol
	if p == 7 || p == 9 {
		s.C.SetProtocol(p - 1)
		return []string{fmt.Sprintf("ATSP%d", p-1)}
	}
	return nil
}

func (s *Scanner) protocol29() []string {
	p := s.C.Format().Protocol
	if p == 6 || p == 8 {
		s.C.SetProtocol(p + 1)
		return []string{fmt.Sprintf("ATSP%d", p+1)}
	}
	return nil
}

// request sends a service request to the configured module and returns
// the positive payload, or the NRC of a negative answer. Response-pending
// (0x78) is retried.
func (s *Scanner) request(req string, positive byte) ([]byte, byte, error) {
	for attempt := 0; attempt < 4; attempt++ {
		resp, err := s.C.QueryTimeout(req, s.Timeout)
		if err != nil {
			return nil, 0, err
		}
		var nrc byte
		for _, r := range resp {
			if len(r.Data) > 0 && r.Data[0] == positive {
				return r.Data, 0, nil
			}
			if len(r.Data) >= 3 && r.Data[0] == 0x7F {
				nrc = r.Data[2]
			}
		}
		if nrc == 0x78 || nrc == 0x21 {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		if nrc != 0 {
			return nil, nrc, nil
		}
		return nil, 0, elm.ErrNoData
	}
	return nil, 0x78, nil
}

// Scan reads DTCs (and identification) from one module.
func (s *Scanner) Scan(m Module, identify bool) Result {
	start := time.Now()
	r := Result{Module: m}
	defer func() { r.Duration = time.Since(start) }()
	if err := s.address(m); err != nil {
		r.Err = err
		return r
	}
	data, nrc, err := s.request("1902FF", 0x59)
	switch {
	case errors.Is(err, elm.ErrNoData):
		r.State = Absent
		return r
	case err != nil:
		r.Err = err
		return r
	case nrc == 0x11 || nrc == 0x7F:
		// UDS not available: try KWP2000 readDTCByStatus (older BMW/VAG).
		kd, knrc, kerr := s.request("1802FF00", 0x58)
		if kerr == nil && knrc == 0 {
			r.State, r.KWP, r.DTCs = OK, true, decodeKWP(kd)
			return r
		}
		r.State, r.NRC = Refused, nrc
		return r
	case nrc != 0:
		r.State, r.NRC = Refused, nrc
		return r
	}
	r.State = OK
	r.DTCs = decodeUDS(data)
	if identify {
		r.PartNo = s.readDID(0xF187)
		r.SysName = s.readDID(0xF197)
	}
	return r
}

func (s *Scanner) readDID(did uint16) string {
	data, _, err := s.request(fmt.Sprintf("22%04X", did), 0x62)
	if err != nil || len(data) < 3 {
		return ""
	}
	var b strings.Builder
	for _, c := range data[3:] {
		if c >= 0x20 && c < 0x7F {
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

// Clear erases DTCs in one module. Some modules only accept it in the
// extended diagnostic session, so that is tried on refusal.
func (s *Scanner) Clear(m Module, kwp bool) error {
	if err := s.address(m); err != nil {
		return err
	}
	req, pos := "14FFFFFF", byte(0x54)
	if kwp {
		req = "14FF00"
	}
	_, nrc, err := s.request(req, pos)
	if err != nil {
		return err
	}
	if nrc == 0 {
		return nil
	}
	if nrc == 0x7F || nrc == 0x7E || nrc == 0x22 || nrc == 0x33 {
		if _, n2, err := s.request("1003", 0x50); err == nil && n2 == 0 {
			_, nrc, err = s.request(req, pos)
			_, _, _ = s.request("1001", 0x50)
			if err != nil {
				return err
			}
			if nrc == 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("module refused clear: %s", NRCText(nrc))
}

// Restore puts the adapter back into plain OBD mode.
func (s *Scanner) Restore() error {
	s.C.SetExtAddr(false)
	s.cur, s.extOn = "", false
	return s.C.Initialize()
}

// decodeUDS parses a 59 02 response: [mask] then 4-byte records.
func decodeUDS(d []byte) []DTC {
	var out []DTC
	if len(d) < 3 {
		return nil
	}
	for i := 3; i+4 <= len(d); i += 4 {
		code := obd.DecodeDTC(d[i], d[i+1])
		if code == "" && d[i+2] == 0 {
			continue
		}
		if code == "" {
			code = "P0000"
		}
		out = append(out, DTC{Code: code, FTB: d[i+2], Status: d[i+3], Desc: obd.Describe(code)})
	}
	return out
}

// decodeKWP parses a 58 response: count then 3-byte records (hi lo status).
func decodeKWP(d []byte) []DTC {
	var out []DTC
	for i := 2; i+3 <= len(d); i += 3 {
		if d[i] == 0 && d[i+1] == 0 {
			continue
		}
		// KWP status bits are manufacturer-specific; report the code as stored.
		out = append(out, DTC{Code: fmt.Sprintf("%02X%02X", d[i], d[i+1]), Status: StConfirmed, KWP: true,
			Desc: fmt.Sprintf("Manufacturer-specific code (KWP2000, status %02X) — look up for this make", d[i+2])})
	}
	return out
}
