package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExclusiveJobsBlockEachOtherAcrossKinds(t *testing.T) {
	m := NewManager("")
	release := make(chan struct{})

	_, err := m.Start("talos-upgrade", "upgrade", true, func(ctx context.Context, _ func(string, ...any)) error {
		<-release

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Start("config-rollout", "rollout", true, func(context.Context, func(string, ...any)) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy while another exclusive job runs, got %v", err)
	}

	// Non-exclusive jobs (e.g. a single reboot) may still run.
	if _, err := m.Start("reboot", "reboot", false, func(context.Context, func(string, ...any)) error { return nil }); err != nil {
		t.Fatalf("non-exclusive job blocked: %v", err)
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for m.Running() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := m.Start("config-rollout", "rollout", true, func(context.Context, func(string, ...any)) error { return nil }); err != nil {
		t.Fatalf("exclusive job should start once the other finished: %v", err)
	}
}

func TestJobLogAndStatus(t *testing.T) {
	m := NewManager(t.TempDir())

	j, err := m.Start("x", "title", false, func(_ context.Context, log func(string, ...any)) error {
		log("hello %d", 1)

		return errors.New("bad")
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := m.Get(j.ID); got.Status != StatusRunning {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	got, _ := m.Get(j.ID)
	if got.Status != StatusFailed || got.Error != "bad" || len(got.Log) < 1 || got.Log[0].Msg != "hello 1" {
		t.Fatalf("unexpected job: %+v", got)
	}
}
