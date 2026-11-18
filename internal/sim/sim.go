// Package sim is a fake ELM327 on a CAN 11-bit/500k car, used for --sim and tests.
// It implements elm.Port.
package sim

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Sim is an in-memory ELM327 + vehicle.
type Sim struct {
	mu      sync.Mutex
	in      strings.Builder
	out     []byte
	ready   chan struct{}
	timeout time.Duration
	closed  bool
	start   time.Time
	rng     *rand.Rand

	// Latency simulates bus + adapter round-trip.
	Latency time.Duration

	echo, spaces, headers bool
	stored, pending       []uint16
	permanent             []uint16

	// UDS addressing state set by ATSH / ATCP / ATCRA.
	sh, cp, cra string
	session     byte
	modules     map[string]*module // by request header
}

// module is a simulated UDS control unit.
type module struct {
	rx         string
	name, part string
	dtcs       [][4]byte // hi, mid, ftb, status
	refuse19   byte      // NRC returned for 19 xx, 0 = none
	clearNeeds byte      // session required to clear, 0 = any
}

// New returns a simulator in the power-on state.
func New() *Sim {
	s := &Sim{
		ready:     make(chan struct{}, 1),
		timeout:   time.Second,
		start:     time.Now(),
		rng:       rand.New(rand.NewSource(1)),
		Latency:   25 * time.Millisecond,
		stored:    []uint16{0x0133, 0x0420},
		pending:   []uint16{0x0301},
		permanent: []uint16{0x0420},
	}
	engine := &module{rx: "7E8", name: "R4 2.0l TDI CR", part: "04L906026FN",
		dtcs: [][4]byte{{0x01, 0x33, 0x00, 0x09}, {0x04, 0x20, 0x00, 0x08}}}
	trans := &module{rx: "7E9", name: "DQ381", part: "0D9300012"}
	s.modules = map[string]*module{
		"7E0": engine,
		"7E1": trans,
		"710": {rx: "77A", name: "GW MQB High", part: "5Q0907530AC"},
		"713": {rx: "77D", name: "ESC", part: "5Q0907379AN", clearNeeds: 0x03,
			dtcs: [][4]byte{{0x40, 0x35, 0x1C, 0x09}, {0xC1, 0x00, 0x87, 0x08}}},
		"714": {rx: "77E", name: "KOMBI", part: "5E0920740B", dtcs: [][4]byte{{0xC1, 0x21, 0x87, 0x2C}}},
		"715": {rx: "77F", name: "AIRBAG VW20", part: "5Q0959655BG"},
		"70E": {rx: "778", name: "BCM MQBAB", part: "5Q0937084CQ", refuse19: 0x22},
		// Same engine and gearbox reachable via 29-bit normal fixed addressing.
		"18DA10F1": {rx: "18DAF110", name: engine.name, part: engine.part, dtcs: engine.dtcs},
		"18DA18F1": {rx: "18DAF118", name: trans.name, part: trans.part},
	}
	s.reset()
	return s
}

func (s *Sim) reset() {
	s.echo, s.spaces, s.headers = true, true, false
	s.sh, s.cp, s.cra, s.session = "", "18", "", 1
}

// Write accepts command bytes; each '\r' terminates a command.
func (s *Sim) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, fmt.Errorf("port closed")
	}
	var cmds []string
	for _, c := range p {
		if c == '\r' {
			cmds = append(cmds, s.in.String())
			s.in.Reset()
			continue
		}
		if c != '\n' {
			s.in.WriteByte(c)
		}
	}
	s.mu.Unlock()
	for _, c := range cmds {
		c := c
		time.AfterFunc(s.Latency, func() { s.emit(c) })
	}
	return len(p), nil
}

func (s *Sim) emit(cmd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	if s.echo {
		b.WriteString(cmd + "\r")
	}
	for _, l := range s.respond(cmd) {
		b.WriteString(l + "\r")
	}
	b.WriteString("\r>")
	s.out = append(s.out, b.String()...)
	select {
	case s.ready <- struct{}{}:
	default:
	}
}

// Read blocks until data is available or the read timeout elapses.
func (s *Sim) Read(p []byte) (int, error) {
	deadline := time.Now().Add(s.timeout)
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return 0, fmt.Errorf("port closed")
		}
		if len(s.out) > 0 {
			n := copy(p, s.out)
			s.out = s.out[n:]
			s.mu.Unlock()
			return n, nil
		}
		s.mu.Unlock()
		wait := time.Until(deadline)
		if wait <= 0 {
			return 0, nil
		}
		select {
		case <-s.ready:
		case <-time.After(wait):
			return 0, nil
		}
	}
}

func (s *Sim) SetReadTimeout(d time.Duration) error {
	s.mu.Lock()
	s.timeout = d
	s.mu.Unlock()
	return nil
}

func (s *Sim) ResetInputBuffer() error {
	s.mu.Lock()
	s.out = nil
	s.mu.Unlock()
	return nil
}

func (s *Sim) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

// --- adapter behaviour ---

func (s *Sim) respond(raw string) []string {
	cmd := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	if cmd == "" {
		return nil
	}
	if strings.HasPrefix(cmd, "AT") {
		return s.at(cmd[2:])
	}
	if len(cmd)%2 == 1 || !isHex(cmd) {
		return []string{"?"}
	}
	return s.obd(cmd)
}

func (s *Sim) at(c string) []string {
	switch {
	case c == "Z":
		s.reset()
		return []string{"", "ELM327 v1.5"}
	case c == "WS":
		s.reset()
		return []string{"ELM327 v1.5"}
	case c == "I":
		return []string{"ELM327 v1.5"}
	case c == "@1":
		return []string{"lazyobd simulator"}
	case c == "E0", c == "E1":
		s.echo = c == "E1"
	case c == "S0", c == "S1":
		s.spaces = c == "S1"
	case c == "H0", c == "H1":
		s.headers = c == "H1"
	case strings.HasPrefix(c, "SH"):
		s.sh = strings.TrimSpace(c[2:])
	case strings.HasPrefix(c, "CP"):
		s.cp = strings.TrimSpace(c[2:])
	case strings.HasPrefix(c, "CRA"):
		s.cra = strings.TrimSpace(c[3:])
	case strings.HasPrefix(c, "CEA"), strings.HasPrefix(c, "FCS"), strings.HasPrefix(c, "AR"):
	case c == "DPN":
		return []string{"A6"}
	case c == "DP":
		return []string{"AUTO, ISO 15765-4 (CAN 11/500)"}
	case c == "RV":
		return []string{fmt.Sprintf("%.1fV", 13.9+0.2*math.Sin(s.t()/7))}
	case strings.HasPrefix(c, "L"), strings.HasPrefix(c, "SP"), strings.HasPrefix(c, "TP"),
		strings.HasPrefix(c, "AT"), strings.HasPrefix(c, "ST"), c == "D", strings.HasPrefix(c, "CAF"),
		strings.HasPrefix(c, "M"):
	default:
		return []string{"?"}
	}
	return []string{"OK"}
}

func (s *Sim) t() float64 { return time.Since(s.start).Seconds() }

func (s *Sim) obd(cmd string) []string {
	var b []byte
	for i := 0; i < len(cmd); i += 2 {
		var v byte
		fmt.Sscanf(cmd[i:i+2], "%02X", &v)
		b = append(b, v)
	}
	if s.sh != "" && s.sh != "7DF" {
		return s.physical(b)
	}
	switch b[0] {
	case 0x01:
		if len(b) < 2 {
			return []string{"NO DATA"}
		}
		var lines []string
		if d := s.pid(b[1]); d != nil {
			lines = append(lines, s.frames("7E8", append([]byte{0x41, b[1]}, d...))...)
		}
		// The transmission answers the support bitmap too.
		if b[1] == 0x00 {
			lines = append(lines, s.frames("7E9", []byte{0x41, 0x00, 0x98, 0x18, 0x00, 0x00})...)
		}
		if len(lines) == 0 {
			return []string{"NO DATA"}
		}
		return lines
	case 0x03:
		return s.frames("7E8", dtcPayload(0x43, s.stored))
	case 0x07:
		return s.frames("7E8", dtcPayload(0x47, s.pending))
	case 0x0A:
		return s.frames("7E8", dtcPayload(0x4A, s.permanent))
	case 0x04:
		s.stored, s.pending = nil, nil
		return s.frames("7E8", []byte{0x44})
	case 0x09:
		if len(b) < 2 {
			return []string{"NO DATA"}
		}
		switch b[1] {
		case 0x00:
			return s.frames("7E8", []byte{0x49, 0x00, 0x55, 0x40, 0x00, 0x00})
		case 0x02:
			return s.frames("7E8", append([]byte{0x49, 0x02, 0x01}, "TMBJJ7NE8L0123456"...))
		case 0x04:
			return s.frames("7E8", append([]byte{0x49, 0x04, 0x01}, pad("03L906018JL", 16)...))
		case 0x0A:
			l := s.frames("7E8", append([]byte{0x49, 0x0A, 0x01}, pad("ECM\x00-EngineControl", 20)...))
			return append(l, s.frames("7E9", append([]byte{0x49, 0x0A, 0x01}, pad("TCM\x00-TransmisCtrl", 20)...))...)
		}
	}
	return []string{"NO DATA"}
}

func pad(s string, n int) []byte {
	b := []byte(s)
	for len(b) < n {
		b = append(b, 0)
	}
	return b[:n]
}

func dtcPayload(svc byte, codes []uint16) []byte {
	p := []byte{svc, byte(len(codes))}
	for _, c := range codes {
		p = append(p, byte(c>>8), byte(c))
	}
	return p
}

var supported = []byte{0x01, 0x04, 0x05, 0x06, 0x07, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x14, 0x15, 0x1F,
	0x20, 0x21, 0x2F, 0x31, 0x33, 0x3C, 0x40, 0x42, 0x46, 0x49, 0x4C, 0x5C, 0x5E}

func bitmap(base byte) []byte {
	var bits uint32
	for _, p := range supported {
		if p > base && p <= base+32 {
			bits |= 1 << (31 - (p - base - 1))
		}
	}
	return []byte{byte(bits >> 24), byte(bits >> 16), byte(bits >> 8), byte(bits)}
}

func u16(v float64) []byte {
	if v < 0 {
		v = 0
	}
	if v > 65535 {
		v = 65535
	}
	n := uint16(v)
	return []byte{byte(n >> 8), byte(n)}
}

func clampB(v float64) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// pid returns simulated data bytes for a Mode 01 PID, nil if unsupported.
func (s *Sim) pid(p byte) []byte {
	t := s.t()
	noise := func(a float64) float64 { return (s.rng.Float64()*2 - 1) * a }
	// Driving profile: idle → accelerate → cruise, repeating every 60s.
	phase := math.Mod(t, 60) / 60
	throttle := 8 + 70*math.Max(0, math.Sin(phase*2*math.Pi)) + noise(2)
	rpm := 780 + throttle*45 + noise(20)
	speed := math.Max(0, 110*math.Sin(phase*math.Pi)+noise(1))
	coolant := 90 - 70*math.Exp(-t/90)
	switch p {
	case 0x00, 0x20, 0x40:
		return bitmap(p)
	case 0x01:
		mil := byte(0)
		if len(s.stored) > 0 {
			mil = 0x80
		}
		return []byte{mil | byte(len(s.stored)), 0x07, 0x65, 0x21}
	case 0x04:
		return []byte{clampB(throttle * 1.2 * 2.55)}
	case 0x05:
		return []byte{clampB(coolant + 40)}
	case 0x06:
		return []byte{clampB(128 + noise(6))}
	case 0x07:
		return []byte{clampB(131)}
	case 0x0B:
		return []byte{clampB(30 + throttle*0.9)}
	case 0x0C:
		return u16(rpm * 4)
	case 0x0D:
		return []byte{clampB(speed)}
	case 0x0E:
		return []byte{clampB((12 + noise(3) + 64) * 2)}
	case 0x0F:
		return []byte{clampB(28 + noise(0.5) + 40)}
	case 0x10:
		return u16((2.5 + throttle*0.6) * 100)
	case 0x11:
		return []byte{clampB(throttle * 2.55)}
	case 0x14:
		return []byte{clampB((0.45 + 0.4*math.Sin(t*3)) * 200), 0xFF}
	case 0x15:
		return []byte{clampB(0.68 * 200), 0xFF}
	case 0x1F:
		return u16(t)
	case 0x21:
		return u16(42)
	case 0x2F:
		return []byte{clampB((61 - t/120) * 2.55)}
	case 0x31:
		return u16(1234)
	case 0x33:
		return []byte{101}
	case 0x3C:
		return u16((420 + throttle*3 + 40) * 10)
	case 0x42:
		return u16((13.9 + noise(0.05)) * 1000)
	case 0x46:
		return []byte{clampB(14 + 40)}
	case 0x49:
		return []byte{clampB((throttle + 6) * 2.55)}
	case 0x4C:
		return []byte{clampB(throttle * 2.55)}
	case 0x5C:
		return []byte{clampB(coolant - 5 + 40)}
	case 0x5E:
		return u16((0.8 + throttle*0.15) * 20)
	}
	return nil
}

// frames formats a payload as the ELM would print it, honouring the
// headers/spaces settings, splitting into ISO-TP frames as needed.
func (s *Sim) frames(hdr string, payload []byte) []string {
	hexs := func(b []byte) string {
		parts := make([]string, len(b))
		for i, v := range b {
			parts[i] = fmt.Sprintf("%02X", v)
		}
		sep := ""
		if s.spaces {
			sep = " "
		}
		return strings.Join(parts, sep)
	}
	join := func(h string, b []byte) string {
		if s.spaces {
			return h + " " + hexs(b)
		}
		return h + hexs(b)
	}
	padTo8 := func(b []byte) []byte {
		for len(b) < 8 {
			b = append(b, 0xAA)
		}
		return b
	}
	if len(payload) <= 7 {
		if !s.headers {
			return []string{hexs(payload)}
		}
		return []string{join(hdr, padTo8(append([]byte{byte(len(payload))}, payload...)))}
	}
	var out []string
	if !s.headers {
		out = append(out, fmt.Sprintf("%03X", len(payload)))
		rest := payload
		for i := 0; len(rest) > 0; i++ {
			n := 7
			if i == 0 {
				n = 6
			}
			if n > len(rest) {
				n = len(rest)
			}
			out = append(out, fmt.Sprintf("%X: %s", i%16, hexs(rest[:n])))
			rest = rest[n:]
		}
		return out
	}
	out = append(out, join(hdr, append([]byte{0x10 | byte(len(payload)>>8), byte(len(payload))}, payload[:6]...)))
	rest := payload[6:]
	for seq := 1; len(rest) > 0; seq++ {
		n := 7
		if n > len(rest) {
			n = len(rest)
		}
		out = append(out, join(hdr, padTo8(append([]byte{0x20 | byte(seq&0xF)}, rest[:n]...))))
		rest = rest[n:]
	}
	return out
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789ABCDEF", r) {
			return false
		}
	}
	return true
}

// physical answers a request addressed to one module via ATSH.
func (s *Sim) physical(b []byte) []string {
	hdr := s.sh
	if len(hdr) == 6 { // 29-bit: ATCP supplies the priority byte
		hdr = s.cp + hdr
	}
	m := s.modules[hdr]
	if m == nil || (s.cra != "" && s.cra != m.rx) {
		return []string{"NO DATA"}
	}
	neg := func(nrc byte) []string { return s.frames(m.rx, []byte{0x7F, b[0], nrc}) }
	switch b[0] {
	case 0x3E:
		return s.frames(m.rx, []byte{0x7E, 0x00})
	case 0x10:
		if len(b) < 2 {
			return neg(0x13)
		}
		s.session = b[1]
		return s.frames(m.rx, []byte{0x50, b[1], 0x00, 0x32, 0x01, 0xF4})
	case 0x19:
		if m.refuse19 != 0 {
			return neg(m.refuse19)
		}
		p := []byte{0x59, 0x02, 0xFF}
		for _, d := range m.dtcs {
			p = append(p, d[:]...)
		}
		return s.frames(m.rx, p)
	case 0x14:
		if m.clearNeeds != 0 && s.session != m.clearNeeds {
			return neg(0x7F)
		}
		m.dtcs = nil
		if m.rx == "7E8" || m.rx == "18DAF110" {
			s.modules["7E0"].dtcs, s.modules["18DA10F1"].dtcs = nil, nil
			s.stored, s.pending = nil, nil
		}
		return s.frames(m.rx, []byte{0x54})
	case 0x22:
		if len(b) < 3 {
			return neg(0x13)
		}
		var v string
		switch uint16(b[1])<<8 | uint16(b[2]) {
		case 0xF187:
			v = m.part
		case 0xF197:
			v = m.name
		default:
			return neg(0x31)
		}
		return s.frames(m.rx, append([]byte{0x62, b[1], b[2]}, v...))
	case 0x01, 0x03, 0x09:
		if m.rx == "7E8" {
			saved := s.sh
			s.sh = ""
			defer func() { s.sh = saved }()
			return s.obd(fmt.Sprintf("%X", b))
		}
	}
	return neg(0x11)
}
