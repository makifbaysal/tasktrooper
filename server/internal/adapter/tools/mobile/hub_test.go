package mobile

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// keeper stands in for appiumhub.Manager: it manages every address it is
// asked about and counts the calls that would have kept the hub up.
type keeper struct {
	mu      sync.Mutex
	ensured []string
	fail    error
	manages bool
}

func (k *keeper) Ensure(_ context.Context, hubURL string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.ensured = append(k.ensured, hubURL)
	return k.fail
}

func (k *keeper) Manages(string) bool { return k.manages }

func (k *keeper) calls() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.ensured)
}

func sessionWithKeeper(t *testing.T, h *hub, k *keeper) *Session {
	t.Helper()
	s := newTestSession(t, h, "")
	s.setHub(k)
	return s
}

func TestEveryToolCallEnsuresTheHubFirst(t *testing.T) {
	h := newHub()
	k := &keeper{manages: true}
	s := sessionWithKeeper(t, h, k)

	require.False(t, execTool(t, newTapTool(s), `{"x":1,"y":1}`).IsError)
	require.False(t, execTool(t, newTapTool(s), `{"x":2,"y":2}`).IsError)

	assert.Equal(t, 2, k.calls(), "a call on a held lease is use of the hub too")
	assert.True(t, h.saw("POST /session"))
}

func TestAHubThatWillNotStartIsRefusedInTheCatalogsWords(t *testing.T) {
	h := newHub()
	k := &keeper{manages: true, fail: errors.New("appium exited before it answered on http://127.0.0.1:4723 (exit status 1)")}
	s := sessionWithKeeper(t, h, k)

	res := execTool(t, newTapTool(s), `{"x":1,"y":1}`)

	assert.True(t, res.IsError)
	assert.Nil(t, res.ResourceBlock, "a broken hub is not a busy device: nothing would ever resume the task")
	assert.Equal(t, prompt.MobileHubUnavailableText(k.fail.Error()), res.Content)
	assert.Contains(t, res.Content, "exit status 1")
	assert.False(t, h.saw("POST /session"))
}

func TestWaitForGivesUpAtOnceOnAHubThatWillNotStart(t *testing.T) {
	h := newHub()
	k := &keeper{manages: true, fail: errors.New("appium did not answer")}
	s := sessionWithKeeper(t, h, k)

	res := execTool(t, newWaitForTool(s), `{"text":"Save","timeout_seconds":30}`)

	assert.True(t, res.IsError)
	assert.Equal(t, 1, k.calls(), "polled a hub that will not start until the deadline")
}

func TestProbeReadsAStoppedOnDemandHubAsAFreeDevice(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	down := "http://127.0.0.1:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	require.NoError(t, ln.Close())

	onDemand := NewSession(Config{HubURL: down, DeviceUDID: "emulator-5554"})
	onDemand.setHub(&keeper{manages: true})
	assert.True(t, onDemand.Probe(context.Background()),
		"a parked task would wait forever for a hub only its own run starts")

	k := &keeper{manages: false}
	external := NewSession(Config{HubURL: down, DeviceUDID: "emulator-5554"})
	external.setHub(k)
	assert.False(t, external.Probe(context.Background()), "a hub somebody else runs that is down is not a free device")

	assert.Zero(t, k.calls(), "a probe started a hub")
}

func TestPoolInUseFollowsTheLease(t *testing.T) {
	p, _ := poolOf(t, "127.0.0.1:5555")
	assert.False(t, p.InUse())

	run := runCtx(t)
	_, err := p.acquire(run)
	require.NoError(t, err)
	assert.True(t, p.InUse())

	p.Release(run)
	assert.False(t, p.InUse())
}

func TestPhonesRegisteredAfterUseHubGetTheHubToo(t *testing.T) {
	k := &keeper{manages: true}
	p := NewPool()
	t.Cleanup(p.Close)
	p.UseHub(k)

	h := newHub()
	srv := h.serve(t)
	p.Reconfigure([]Config{{HubURL: srv.URL, DeviceUDID: "127.0.0.1:5555"}})
	require.NoError(t, p.run(runCtx(t), "GET", "/screenshot", nil, nil))

	require.NotEmpty(t, k.ensured)
	for _, url := range k.ensured {
		assert.Equal(t, srv.URL, url)
	}
}
