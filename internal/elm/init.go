package elm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

// Bauds tried, in order, when the baud rate is not given. 38400 is the
// ELM327 default; many clones ship at 9600 or 115200.
var Bauds = []int{38400, 9600, 115200, 57600, 230400, 500000}

// DetectPorts lists likely adapter device paths, best guesses first.
func DetectPorts() []string {
	var out []string
	byID, _ := filepath.Glob("/dev/serial/by-id/*")
	sort.Strings(byID)
	out = append(out, byID...)
	for _, pat := range []string{"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/rfcomm*"} {
		m, _ := filepath.Glob(pat)
		sort.Strings(m)
		out = append(out, m...)
	}
	return out
}

// PermissionHint returns a human hint when path exists but cannot be opened.
func PermissionHint(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		f.Close()
		return ""
	}
	if !errors.Is(err, os.ErrPermission) {
		return ""
	}
	grp := "uucp/dialout"
	if st, ok := statGroup(fi); ok {
		grp = st
	}
	return fmt.Sprintf("permission denied on %s — run: sudo usermod -aG %s $USER  (then log out and back in)", path, grp)
}

// OpenSerial opens path and finds a baud rate the adapter answers on.
// baud == 0 means auto-detect.
func OpenSerial(path string, baud int) (*Conn, error) {
	if path == "" {
		ports := DetectPorts()
		if len(ports) == 0 {
			return nil, errors.New("no serial adapter found (looked for /dev/serial/by-id/*, /dev/ttyUSB*, /dev/ttyACM*) — is the cable plugged in?")
		}
		path = ports[0]
	}
	bauds := Bauds
	if baud > 0 {
		bauds = []int{baud}
	}
	var lastErr error
	for _, b := range bauds {
		p, err := serial.Open(path, &serial.Mode{BaudRate: b, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit})
		if err != nil {
			if h := PermissionHint(path); h != "" {
				return nil, errors.New(h)
			}
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		c := NewConn(p)
		// Flush any half-typed command with a harmless ATI. Never send a bare
		// CR: the ELM327 repeats the previous command on an empty line.
		_, _ = c.Send("ATI", 400*time.Millisecond)
		resp, err := c.Send("ATI", 1500*time.Millisecond)
		if err == nil && (strings.Contains(strings.ToUpper(resp), "ELM") || strings.Contains(resp, "OK") || strings.TrimSpace(resp) != "") {
			c.Info.Port = path
			c.Info.Baud = b
			return c, nil
		}
		lastErr = err
		p.Close()
	}
	if lastErr == nil {
		lastErr = ErrTimeout
	}
	return nil, fmt.Errorf("%s: adapter did not answer at any baud rate (%v)", path, lastErr)
}

// Initialize resets the adapter, configures output, and connects to the
// vehicle bus with automatic protocol search.
func (c *Conn) Initialize() error {
	ver, err := c.AT("ATZ", 4*time.Second)
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	for _, l := range strings.Split(ver, "\n") {
		if strings.Contains(strings.ToUpper(l), "ELM") || strings.Contains(strings.ToUpper(l), "STN") {
			c.Info.Version = strings.TrimSpace(l)
		}
	}
	for _, cmd := range []string{"ATE0", "ATL0", "ATS0", "ATH1", "ATAT1", "ATSP0"} {
		if _, err := c.AT(cmd, 2*time.Second); err != nil {
			return fmt.Errorf("%s: %w", cmd, err)
		}
	}
	c.mu.Lock()
	c.format = Format{Headers: true}
	c.mu.Unlock()

	// The first request triggers the protocol search, which can take a while.
	raw, err := c.Send("0100", 15*time.Second)
	if err != nil {
		return fmt.Errorf("connecting to vehicle: %w", err)
	}
	for _, l := range CleanLines(raw, "0100") {
		if e, ok := lineError(l); ok && (e == ErrUnableToConnect || e == ErrBusInit || e == ErrCAN) {
			return fmt.Errorf("%s — is the ignition on?", e)
		}
	}
	dpn, err := c.AT("ATDPN", 2*time.Second)
	if err != nil {
		return err
	}
	dpn = strings.TrimPrefix(strings.TrimSpace(strings.ToUpper(dpn)), "A")
	if n, err := strconv.ParseInt(dpn, 16, 8); err == nil {
		c.mu.Lock()
		c.format.Protocol = int(n)
		c.mu.Unlock()
		c.Info.Protocol = int(n)
	}
	if dp, err := c.AT("ATDP", 2*time.Second); err == nil {
		c.Info.ProtocolName = strings.TrimPrefix(dp, "AUTO, ")
	}
	return nil
}

// Voltage reads the supply voltage at the OBD port via ATRV.
func (c *Conn) Voltage() (float64, error) {
	s, err := c.AT("ATRV", time.Second)
	if err != nil {
		return 0, err
	}
	s = strings.TrimSuffix(strings.TrimSpace(strings.ToUpper(s)), "V")
	return strconv.ParseFloat(s, 64)
}
