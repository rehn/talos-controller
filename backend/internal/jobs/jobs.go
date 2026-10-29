// Package jobs runs long operations (upgrades, reboots) in the background
// and keeps their logs.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Status values.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Line is one log line.
type Line struct {
	Time time.Time `json:"time"`
	Msg  string    `json:"msg"`
}

// Job is a background operation.
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Log        []Line     `json:"log"`
	Exclusive  bool       `json:"exclusive,omitempty"`

	cancel context.CancelFunc
}

// Manager tracks jobs.
type Manager struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	path    string
	maxKeep int
}

// ErrBusy is returned when an exclusive job of the same kind is running.
var ErrBusy = errors.New("another job of this kind is already running")

// NewManager creates a manager persisting history under dataDir (optional).
func NewManager(dataDir string) *Manager {
	m := &Manager{jobs: map[string]*Job{}, maxKeep: 50}

	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0o750); err == nil {
			m.path = filepath.Join(dataDir, "jobs.json")
			m.load()
		} else {
			slog.Warn("job history will not be persisted", "err", err)
		}
	}

	return m
}

func (m *Manager) load() {
	b, err := os.ReadFile(m.path)
	if err != nil {
		return
	}

	var list []*Job
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}

	for _, j := range list {
		if j.Status == StatusRunning {
			// The process restarted while the job was running.
			j.Status = StatusFailed
			j.Error = "interrupted by restart"
			now := time.Now()
			j.FinishedAt = &now
		}

		m.jobs[j.ID] = j
	}
}

// persist must be called with m.mu held.
func (m *Manager) persist() {
	if m.path == "" {
		return
	}

	list := m.sortedLocked()
	if len(list) > m.maxKeep {
		for _, j := range list[m.maxKeep:] {
			if j.Status != StatusRunning {
				delete(m.jobs, j.ID)
			}
		}

		list = list[:m.maxKeep]
	}

	b, err := json.Marshal(list)
	if err != nil {
		return
	}

	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err == nil {
		_ = os.Rename(tmp, m.path)
	}
}

func (m *Manager) sortedLocked() []*Job {
	list := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		list = append(list, j)
	}

	sort.Slice(list, func(i, k int) bool { return list[i].StartedAt.After(list[k].StartedAt) })

	return list
}

// Start launches fn in the background. Exclusive jobs (rolling upgrades and
// rollouts) never run at the same time as another exclusive job.
func (m *Manager) Start(kind, title string, exclusive bool, fn func(ctx context.Context, log func(string, ...any)) error) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if exclusive {
		for _, j := range m.jobs {
			if j.Exclusive && j.Status == StatusRunning {
				return nil, ErrBusy
			}
		}
	}

	id := make([]byte, 6)
	_, _ = rand.Read(id)

	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{
		ID:        hex.EncodeToString(id),
		Kind:      kind,
		Title:     title,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		Log:       []Line{},
		Exclusive: exclusive,
		cancel:    cancel,
	}
	m.jobs[j.ID] = j
	m.persist()

	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		slog.Info("job", "id", j.ID, "msg", msg)

		m.mu.Lock()
		defer m.mu.Unlock()

		j.Log = append(j.Log, Line{Time: time.Now(), Msg: msg})
		if len(j.Log) > 2000 {
			j.Log = j.Log[len(j.Log)-2000:]
		}

		m.persist()
	}

	go func() {
		defer cancel()

		err := fn(ctx, logf)

		m.mu.Lock()
		defer m.mu.Unlock()

		now := time.Now()
		j.FinishedAt = &now

		switch {
		case err == nil:
			j.Status = StatusSucceeded
		case errors.Is(err, context.Canceled):
			j.Status = StatusCancelled
			j.Error = "cancelled"
		default:
			j.Status = StatusFailed
			j.Error = err.Error()
		}

		j.Log = append(j.Log, Line{Time: now, Msg: "job " + j.Status})
		m.persist()
	}()

	return j, nil
}

// List returns jobs, newest first, without logs.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := []Job{}

	for _, j := range m.sortedLocked() {
		c := *j
		c.Log = nil
		out = append(out, c)
	}

	return out
}

// Get returns a copy of a job.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}

	c := *j
	c.Log = append([]Line(nil), j.Log...)

	return c, true
}

// Cancel cancels a running job.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	j, ok := m.jobs[id]
	if !ok || j.Status != StatusRunning || j.cancel == nil {
		return false
	}

	j.cancel()

	return true
}

// Running returns the number of running jobs.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := 0

	for _, j := range m.jobs {
		if j.Status == StatusRunning {
			n++
		}
	}

	return n
}
