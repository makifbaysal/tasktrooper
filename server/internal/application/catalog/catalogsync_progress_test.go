package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
)

// Holds every skill embedding until the test lets it go, so a sync can be
// looked at while it is ingesting.
type gatedEmbedLLM struct {
	fixingLLMClient
	entered chan struct{}
	release chan struct{}
}

func (g gatedEmbedLLM) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	g.entered <- struct{}{}
	<-g.release
	return nil, nil
}

func TestSyncProgressReportsTheAgentAndSkillsBeingIngested(t *testing.T) {
	store := newMemCatalogStore()
	syncStore := newMemSyncStore()
	dir := t.TempDir()
	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\n",
		"you are the shipper\n",
		map[string]string{
			"ship":  skillDoc("ship", "ship it", "release", "ship body"),
			"check": skillDoc("check", "check it", "release", "check body"),
		},
		nil,
	)
	llm := gatedEmbedLLM{entered: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(store, llm, "")
	reader := &catalogrepo.Reader{Source: dir}

	if got := svc.SyncProgress(); got.Running {
		t.Fatalf("an idle service reports a running sync: %+v", got)
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.SyncFromCatalog(context.Background(), reader, syncStore)
		done <- err
	}()

	waitEntered := func() {
		t.Helper()
		select {
		case <-llm.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("sync never reached a skill embedding")
		}
	}

	waitEntered()
	first := svc.SyncProgress()
	if !first.Running || first.Agent != "shippy" || !first.NewAgent {
		t.Fatalf("progress while creating shippy = %+v", first)
	}
	if first.SkillsTotal != 2 || first.SkillsDone != 0 || first.AgentsTotal != 1 || first.StartedAt == nil {
		t.Fatalf("progress counts before the first skill landed = %+v", first)
	}

	res, started, err := svc.TrySyncFromCatalog(context.Background(), reader, syncStore)
	if started || res != nil || err != nil {
		t.Fatalf("a second sync must not start while one runs: started=%v res=%v err=%v", started, res, err)
	}

	llm.release <- struct{}{}
	waitEntered()
	if got := svc.SyncProgress(); got.SkillsDone != 1 {
		t.Fatalf("progress after the first skill = %+v", got)
	}
	llm.release <- struct{}{}

	if err := <-done; err != nil {
		t.Fatalf("SyncFromCatalog: %v", err)
	}
	if got := svc.SyncProgress(); got.Running || got.Agent != "" || got.SkillsTotal != 0 || len(got.AgentsAdded) != 0 {
		t.Fatalf("a finished sync must leave the tracker idle, got %+v", got)
	}

	res, started, err = svc.TrySyncFromCatalog(context.Background(), reader, syncStore)
	if !started || err != nil || res == nil {
		t.Fatalf("an idle service must start a sync: started=%v res=%v err=%v", started, res, err)
	}
}
