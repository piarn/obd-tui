package elm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Port is the transport underneath a Conn: a real serial port or the simulator.
// Read must return (0, nil) when the read timeout elapses.
type Port interface {
	io.ReadWriteCloser
	SetReadTimeout(time.Duration) error
	ResetInputBuffer() error
}

// ErrTimeout means the adapter never printed its '>' prompt.
var ErrTimeout = errors.New("adapter timed out (no prompt)")

// Info describes the connected adapter and vehicle bus.
type Info struct {
	Port         string
	Baud         int
	Version      string // e.g. "ELM327 v1.5"
	Protocol     int
	ProtocolName string
}

// Conn is a command/response session with an ELM327. It is safe for
// concurrent use, but commands are strictly serialized.
type Conn struct {
	mu      sync.Mutex
	port    Port
	format  Format
	Timeout time.Duration
	Info    Info
}

// NewConn wraps an already-open port.
func NewConn(p Port) *Conn {
	return &Conn{port: p, Timeout: 3 * time.Second}
}

func (c *Conn) Close() error { return c.port.Close() }

// Format returns the output format currently configured on the adapter.
func (c *Conn) Format() Format {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.format
}

// Send writes cmd and returns everything the adapter printed before its
// '>' prompt.
func (c *Conn) Send(cmd string, timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.send(cmd, timeout)
}

func (c *Conn) send(cmd string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = c.Timeout
	}
	_ = c.port.ResetInputBuffer()
	if _, err := c.port.Write([]byte(cmd + "\r")); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := c.port.SetReadTimeout(50 * time.Millisecond); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	chunk := make([]byte, 256)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := c.port.Read(chunk)
		if err != nil {
			return buf.String(), fmt.Errorf("read: %w", err)
		}
		if n == 0 {
			continue
		}
		// Some adapters emit NUL bytes after reset; drop them.
		buf.Write(bytes.ReplaceAll(chunk[:n], []byte{0}, nil))
		if i := bytes.IndexByte(buf.Bytes(), '>'); i >= 0 {
			return string(buf.Bytes()[:i]), nil
		}
	}
	return buf.String(), ErrTimeout
}

// Query sends an OBD request (e.g. "010C") and parses the reply.
func (c *Conn) Query(cmd string) ([]Response, error) {
	return c.QueryTimeout(cmd, 0)
}

// QueryTimeout is Query with an explicit timeout.
func (c *Conn) QueryTimeout(cmd string, timeout time.Duration) ([]Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := c.send(cmd, timeout)
	if err != nil {
		return nil, err
	}
	return Parse(CleanLines(raw, cmd), c.format)
}

// AT sends an AT command and returns its cleaned text reply.
func (c *Conn) AT(cmd string, timeout time.Duration) (string, error) {
	raw, err := c.Send(cmd, timeout)
	if err != nil {
		return "", err
	}
	return strings.Join(CleanLines(raw, cmd), "\n"), nil
}

// Raw sends an arbitrary command for the console and returns cleaned lines.
// If the user toggles headers or protocol by hand, the parser format is
// updated to match.
func (c *Conn) Raw(cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	raw, err := c.Send(cmd, 10*time.Second)
	u := strings.ToUpper(strings.ReplaceAll(cmd, " ", ""))
	c.mu.Lock()
	switch u {
	case "ATH0":
		c.format.Headers = false
	case "ATH1":
		c.format.Headers = true
	}
	c.mu.Unlock()
	return strings.Join(CleanLines(raw, cmd), "\n"), err
}

// IsIOError reports whether err came from the transport (cable unplugged etc.)
// rather than from the adapter or the vehicle.
func IsIOError(err error) bool {
	if err == nil {
		return false
	}
	var e Error
	if errors.As(err, &e) || errors.Is(err, ErrTimeout) {
		return false
	}
	s := err.Error()
	return strings.HasPrefix(s, "read:") || strings.HasPrefix(s, "write:")
}

// ATOK sends an AT command and fails if the adapter rejects it ("?"),
// which is how clones signal an unsupported command.
func (c *Conn) ATOK(cmd string) error {
	r, err := c.AT(cmd, 2*time.Second)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	u := strings.ToUpper(r)
	if strings.Contains(u, "?") || strings.Contains(u, "ERROR") {
		return fmt.Errorf("adapter rejected %s", cmd)
	}
	return nil
}

// SetExtAddr tells the parser whether extended addressing is active.
func (c *Conn) SetExtAddr(on bool) {
	c.mu.Lock()
	c.format.ExtAddr = on
	c.mu.Unlock()
}

// SetProtocol records a manual protocol change (ATSPx) for the parser.
func (c *Conn) SetProtocol(p int) {
	c.mu.Lock()
	c.format.Protocol = p
	c.mu.Unlock()
}
