// Package logcsv records live PID values to a CSV file, one row per poll cycle.
package logcsv

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"time"

	"lazyobd/internal/obd"
)

// Logger writes rows of PID values.
type Logger struct {
	Path  string
	Rows  int
	f     *os.File
	w     *csv.Writer
	pids  []obd.PID
	start time.Time
}

// DefaultPath returns a timestamped file name in the current directory.
func DefaultPath() string {
	return "lazyobd-" + time.Now().Format("20060102-150405") + ".csv"
}

// Start creates path and writes the header row.
func Start(path string, pids []obd.PID) (*Logger, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	l := &Logger{Path: path, f: f, w: csv.NewWriter(f), pids: pids, start: time.Now()}
	hdr := []string{"timestamp", "elapsed_s"}
	for _, p := range pids {
		name := p.Name
		if p.Unit != "" {
			name += " (" + p.Unit + ")"
		}
		hdr = append(hdr, name)
	}
	if err := l.w.Write(hdr); err != nil {
		f.Close()
		return nil, err
	}
	l.w.Flush()
	return l, l.w.Error()
}

// Row writes the latest values; PIDs without a value are left blank.
func (l *Logger) Row(at time.Time, vals map[byte]float64) error {
	rec := []string{at.Format(time.RFC3339Nano), fmt.Sprintf("%.3f", at.Sub(l.start).Seconds())}
	for _, p := range l.pids {
		if v, ok := vals[p.Code]; ok {
			rec = append(rec, strconv.FormatFloat(v, 'f', p.Prec, 64))
		} else {
			rec = append(rec, "")
		}
	}
	if err := l.w.Write(rec); err != nil {
		return err
	}
	l.w.Flush()
	l.Rows++
	return l.w.Error()
}

// Close flushes and closes the file.
func (l *Logger) Close() error {
	l.w.Flush()
	return l.f.Close()
}
