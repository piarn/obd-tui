// Package worker owns the adapter connection. It runs the poll loop and
// serves UI requests one at a time, so only one command is ever on the wire.
package worker

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"lazyobd/internal/elm"
	"lazyobd/internal/obd"
	"lazyobd/internal/uds"
)

// State of the adapter link.
type State int

const (
	Connecting State = iota
	Connected
	Failed
)

// Messages sent to the UI.
type (
	StatusMsg struct {
		State  State
		Detail string
		Info   elm.Info
	}
	SupportedMsg struct {
		PIDs []obd.PID
		Err  error
	}
	ValueMsg struct {
		PID   byte
		Value float64
		Err   error
		At    time.Time
	}
	CycleMsg   struct{ At time.Time }
	VoltageMsg struct{ V float64 }
	DTCMsg     struct {
		Stored, Pending, Permanent []obd.DTC
		Err                        error
	}
	ClearMsg   struct{ Err error }
	MonitorMsg struct {
		Status obd.Status
		Err    error
	}
	VehicleMsg struct {
		Vehicle obd.Vehicle
		Err     error
	}
	RawMsg struct {
		Cmd, Resp string
		Err       error
		Took      time.Duration
	}
	ScanStartMsg struct {
		Total int
		Deep  bool
	}
	ModuleMsg struct {
		Result      uds.Result
		Done, Total int
	}
	ScanDoneMsg struct {
		Err           error
		Cancelled     bool
		NoFlowControl bool
	}
	ModuleClearMsg struct {
		Cleared int
		Err     error
	}
)

// Requests from the UI.
type (
	ReqDTCs      struct{}
	ReqClear     struct{}
	ReqVehicle   struct{}
	ReqRaw       struct{ Cmd string }
	ReqReconnect struct{}
	// ReqScan reads codes from every module in the list (or, if Deep, the
	// full 29-bit address sweep).
	ReqScan struct {
		Modules []uds.Module
		Deep    bool
	}
	// ReqClearModules clears codes in each target, then rescans it.
	ReqClearModules struct{ Targets []ClearTarget }
)

// ClearTarget is one module to clear.
type ClearTarget struct {
	Module uds.Module
	KWP    bool
}

// Opener opens (but does not initialize) an adapter connection.
type Opener func() (*elm.Conn, error)

// Worker drives one adapter.
type Worker struct {
	open   Opener
	reqs   chan any
	Out    chan any
	paused atomic.Bool
	cancel atomic.Bool
	// SlowEvery: non-fast PIDs and voltage are polled every N cycles.
	SlowEvery int
}

// New creates a worker; call Run in a goroutine.
func New(open Opener) *Worker {
	return &Worker{open: open, reqs: make(chan any, 16), Out: make(chan any, 64), SlowEvery: 5}
}

// Request queues a UI request; it is dropped if the queue is full.
func (w *Worker) Request(r any) {
	select {
	case w.reqs <- r:
	default:
	}
}

// CancelScan stops a running module scan after the current module.
func (w *Worker) CancelScan() { w.cancel.Store(true) }

// SetPaused stops or resumes live polling (requests are still served).
func (w *Worker) SetPaused(p bool) { w.paused.Store(p) }

func (w *Worker) emit(ctx context.Context, m any) {
	select {
	case w.Out <- m:
	case <-ctx.Done():
	}
}

var errReconnect = errors.New("reconnect requested")

// Run connects, polls and reconnects until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		w.emit(ctx, StatusMsg{State: Connecting, Detail: "opening adapter…"})
		conn, err := w.open()
		if err == nil {
			w.emit(ctx, StatusMsg{State: Connecting, Detail: "resetting adapter & searching vehicle protocol…", Info: conn.Info})
			if err = conn.Initialize(); err != nil {
				conn.Close()
			}
		}
		if err != nil {
			w.emit(ctx, StatusMsg{State: Failed, Detail: err.Error()})
			w.offline(ctx, 3*time.Second)
			continue
		}
		w.emit(ctx, StatusMsg{State: Connected, Info: conn.Info})
		err = w.session(ctx, conn)
		conn.Close()
		if err != nil && !errors.Is(err, errReconnect) && ctx.Err() == nil {
			w.emit(ctx, StatusMsg{State: Failed, Detail: "link lost: " + err.Error()})
			w.offline(ctx, 2*time.Second)
		}
	}
}

// offline waits before a reconnect, answering requests with errors.
func (w *Worker) offline(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			return
		case r := <-w.reqs:
			if _, ok := r.(ReqReconnect); ok {
				return
			}
			w.reply(ctx, r, errors.New("not connected"))
		}
	}
}

func (w *Worker) reply(ctx context.Context, r any, err error) {
	switch r := r.(type) {
	case ReqDTCs:
		w.emit(ctx, DTCMsg{Err: err})
	case ReqClear:
		w.emit(ctx, ClearMsg{Err: err})
	case ReqVehicle:
		w.emit(ctx, VehicleMsg{Err: err})
	case ReqRaw:
		w.emit(ctx, RawMsg{Cmd: r.Cmd, Err: err})
	case ReqScan:
		w.emit(ctx, ScanDoneMsg{Err: err})
	case ReqClearModules:
		w.emit(ctx, ModuleClearMsg{Err: err})
	}
}

type session struct {
	timeouts int
}

// check classifies an error: returns non-nil if the session must end.
func (s *session) check(err error) error {
	switch {
	case err == nil:
		s.timeouts = 0
	case elm.IsIOError(err):
		return err
	case errors.Is(err, elm.ErrTimeout):
		s.timeouts++
		if s.timeouts >= 3 {
			return err
		}
	}
	return nil
}

func (w *Worker) session(ctx context.Context, c *elm.Conn) error {
	s := &session{}
	sup, err := obd.Supported(c)
	if fatal := s.check(err); fatal != nil {
		return fatal
	}
	pids := obd.SupportedPIDs(sup)
	w.emit(ctx, SupportedMsg{PIDs: pids, Err: err})

	for _, r := range []any{ReqVehicle{}, ReqDTCs{}} {
		if err := w.handle(ctx, c, s, r); err != nil {
			return err
		}
	}

	cycle, idx := 0, 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r := <-w.reqs:
			if err := w.handle(ctx, c, s, r); err != nil {
				return err
			}
			continue
		default:
		}
		if w.paused.Load() || len(pids) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case r := <-w.reqs:
				if err := w.handle(ctx, c, s, r); err != nil {
					return err
				}
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		if idx >= len(pids) {
			idx = 0
			cycle++
			w.emit(ctx, CycleMsg{At: time.Now()})
			if cycle%w.SlowEvery == 0 {
				if v, err := c.Voltage(); err == nil {
					w.emit(ctx, VoltageMsg{V: v})
				} else if fatal := s.check(err); fatal != nil {
					return fatal
				}
			}
			if cycle%(w.SlowEvery*4) == 0 {
				st, err := obd.ReadStatus(c)
				if fatal := s.check(err); fatal != nil {
					return fatal
				}
				w.emit(ctx, MonitorMsg{Status: st, Err: err})
			}
		}
		p := pids[idx]
		idx++
		if cycle > 0 && !p.Fast && cycle%w.SlowEvery != 0 {
			continue
		}
		v, err := obd.Read(c, p)
		if fatal := s.check(err); fatal != nil {
			return fatal
		}
		w.emit(ctx, ValueMsg{PID: p.Code, Value: v, Err: err, At: time.Now()})
	}
}

func (w *Worker) handle(ctx context.Context, c *elm.Conn, s *session, r any) error {
	switch r := r.(type) {
	case ReqReconnect:
		return errReconnect
	case ReqDTCs:
		var m DTCMsg
		var err1, err2 error
		m.Stored, err1 = obd.ReadDTCs(c, obd.Stored)
		m.Pending, err2 = obd.ReadDTCs(c, obd.Pending)
		m.Permanent, _ = obd.ReadDTCs(c, obd.Permanent) // often unsupported
		for _, e := range []error{err1, err2} {
			if fatal := s.check(e); fatal != nil {
				return fatal
			}
			if e != nil && m.Err == nil {
				m.Err = e
			}
		}
		w.emit(ctx, m)
		st, err := obd.ReadStatus(c)
		if fatal := s.check(err); fatal != nil {
			return fatal
		}
		w.emit(ctx, MonitorMsg{Status: st, Err: err})
	case ReqClear:
		err := obd.ClearDTCs(c)
		if fatal := s.check(err); fatal != nil {
			return fatal
		}
		w.emit(ctx, ClearMsg{Err: err})
		return w.handle(ctx, c, s, ReqDTCs{})
	case ReqVehicle:
		v, err := obd.ReadVehicle(c)
		if fatal := s.check(err); fatal != nil {
			return fatal
		}
		w.emit(ctx, VehicleMsg{Vehicle: v, Err: err})
		st, err := obd.ReadStatus(c)
		if fatal := s.check(err); fatal != nil {
			return fatal
		}
		w.emit(ctx, MonitorMsg{Status: st, Err: err})
		if v, err := c.Voltage(); err == nil {
			w.emit(ctx, VoltageMsg{V: v})
		}
	case ReqScan:
		return w.scan(ctx, c, s, r)
	case ReqClearModules:
		return w.clearModules(ctx, c, s, r)
	case ReqRaw:
		start := time.Now()
		resp, err := c.Raw(r.Cmd)
		if fatal := s.check(err); fatal != nil {
			w.emit(ctx, RawMsg{Cmd: r.Cmd, Resp: resp, Err: err, Took: time.Since(start)})
			return fatal
		}
		w.emit(ctx, RawMsg{Cmd: r.Cmd, Resp: resp, Err: err, Took: time.Since(start)})
	}
	return nil
}

func (w *Worker) scan(ctx context.Context, c *elm.Conn, s *session, r ReqScan) error {
	sc, err := uds.NewScanner(c)
	if err != nil {
		w.emit(ctx, ScanDoneMsg{Err: err})
		return s.check(err)
	}
	mods := r.Modules
	if r.Deep {
		mods = uds.SweepModules()
		if err := sc.SetFastTimeout(); err != nil {
			w.emit(ctx, ScanDoneMsg{Err: err})
			return sc.Restore()
		}
	}
	w.cancel.Store(false)
	w.emit(ctx, ScanStartMsg{Total: len(mods), Deep: r.Deep})
	done := ScanDoneMsg{}
	for i, m := range mods {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if w.cancel.Load() {
			done.Cancelled = true
			break
		}
		res := sc.Scan(m, true)
		if fatal := s.check(res.Err); fatal != nil {
			return fatal
		}
		var ae uds.ErrAdapter
		if errors.As(res.Err, &ae) {
			done.Err = res.Err
			break
		}
		w.emit(ctx, ModuleMsg{Result: res, Done: i + 1, Total: len(mods)})
	}
	done.NoFlowControl = sc.NoFlowControl
	if err := sc.Restore(); err != nil {
		return err // adapter is in an unknown state: reconnect
	}
	w.emit(ctx, done)
	return nil
}

func (w *Worker) clearModules(ctx context.Context, c *elm.Conn, s *session, r ReqClearModules) error {
	sc, err := uds.NewScanner(c)
	if err != nil {
		w.emit(ctx, ModuleClearMsg{Err: err})
		return s.check(err)
	}
	msg := ModuleClearMsg{}
	for i, t := range r.Targets {
		if err := sc.Clear(t.Module, t.KWP); err != nil {
			if fatal := s.check(err); fatal != nil {
				return fatal
			}
			if msg.Err == nil {
				msg.Err = fmt.Errorf("%s: %w", t.Module.Name, err)
			}
		} else {
			msg.Cleared++
		}
		res := sc.Scan(t.Module, true)
		if fatal := s.check(res.Err); fatal != nil {
			return fatal
		}
		w.emit(ctx, ModuleMsg{Result: res, Done: i + 1, Total: len(r.Targets)})
	}
	if err := sc.Restore(); err != nil {
		return err
	}
	w.emit(ctx, msg)
	// Engine codes may have changed too.
	return w.handle(ctx, c, s, ReqDTCs{})
}
