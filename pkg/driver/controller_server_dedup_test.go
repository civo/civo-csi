package driver_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/civo/civo-csi/pkg/driver"
	"github.com/civo/civogo"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
)

// fakeWithHooks embeds the real FakeClient and overrides NewVolume / ListVolumes
// so tests can drive the "API rejected our create" race scenarios that the
// stock FakeClient can't express.
type fakeWithHooks struct {
	*civogo.FakeClient

	mu               sync.Mutex
	newVolumeErr     error // if non-nil, NewVolume returns this without touching storage
	newVolumeCalls   int32
	newVolumeBlockOn chan struct{} // if non-nil, NewVolume blocks until this is closed
	listVolumeCalls  int32
}

func (f *fakeWithHooks) NewVolume(v *civogo.VolumeConfig) (*civogo.VolumeResult, error) {
	atomic.AddInt32(&f.newVolumeCalls, 1)
	if f.newVolumeBlockOn != nil {
		<-f.newVolumeBlockOn
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.newVolumeErr != nil {
		return nil, f.newVolumeErr
	}
	return f.FakeClient.NewVolume(v)
}

func (f *fakeWithHooks) ListVolumes() ([]civogo.Volume, error) {
	atomic.AddInt32(&f.listVolumeCalls, 1)
	return f.FakeClient.ListVolumes()
}

// minimalVolumeRequest returns a CreateVolumeRequest with the given name and
// the default size / a single-writer access mode.
func minimalVolumeRequest(name string) *csi.CreateVolumeRequest {
	return &csi.CreateVolumeRequest{
		Name: name,
		VolumeCapabilities: []*csi.VolumeCapability{{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		}},
	}
}

// TestCreateVolume_SingleflightCoalescesConcurrentCalls fires three concurrent
// CreateVolume calls with the same req.Name. Only one (ListVolumes + NewVolume)
// pair should actually run against the API; all three callers must receive the
// same VolumeId.
func TestCreateVolume_SingleflightCoalescesConcurrentCalls(t *testing.T) {
	base, _ := civogo.NewFakeClient()
	gate := make(chan struct{})
	fc := &fakeWithHooks{FakeClient: base, newVolumeBlockOn: gate}

	d, err := driver.NewTestDriver(nil)
	if err != nil {
		t.Fatalf("NewTestDriver: %v", err)
	}
	d.CivoClient = fc

	type result struct {
		resp *csi.CreateVolumeResponse
		err  error
	}
	results := make(chan result, 3)
	for i := 0; i < 3; i++ {
		go func() {
			r, e := d.CreateVolume(context.Background(), minimalVolumeRequest("coalesced-vol"))
			results <- result{resp: r, err: e}
		}()
	}

	// Give the three goroutines time to enter singleflight before releasing
	// the in-flight call. 50 ms is enough for fast test machines; the gate
	// ensures we don't race on the unblock.
	time.Sleep(50 * time.Millisecond)
	close(gate)

	collected := make([]result, 0, 3)
	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			collected = append(collected, r)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for CreateVolume response %d", i)
		}
	}

	for _, r := range collected {
		assert.NoError(t, r.err)
		assert.NotNil(t, r.resp)
	}

	// All three responses must reference the same VolumeId.
	id := collected[0].resp.Volume.VolumeId
	for i, r := range collected[1:] {
		assert.Equal(t, id, r.resp.Volume.VolumeId, "response %d returned a different VolumeId", i+1)
	}

	// Exactly one NewVolume must have been issued; ListVolumes is also called
	// once for the pre-check (the other two callers piggy-backed on the
	// singleflight result).
	assert.Equal(t, int32(1), atomic.LoadInt32(&fc.newVolumeCalls), "expected exactly one NewVolume call")
	assert.Equal(t, int32(1), atomic.LoadInt32(&fc.listVolumeCalls), "expected exactly one ListVolumes call inside singleflight")
}

// TestCreateVolume_DuplicateNameTriggersIdempotentLookup simulates the api-go
// rejecting our NewVolume with database_volume_duplicate_name because a
// concurrent retry already won the race server-side. The CSI plugin must
// resolve the existing volume by name and return it as a success rather than
// bubbling the error back to external-provisioner.
func TestCreateVolume_DuplicateNameTriggersIdempotentLookup(t *testing.T) {
	base, _ := civogo.NewFakeClient()
	// Pre-populate the "existing" volume as if a sibling create had already
	// won server-side.
	existing := civogo.Volume{
		ID:            "existing-vol-id",
		Name:          "raced-vol",
		SizeGigabytes: 10,
		Status:        "available",
	}
	base.Volumes = []civogo.Volume{existing}

	// Wrap with hooks; cause NewVolume to return the duplicate-name error.
	fc := &fakeWithHooks{
		FakeClient:   base,
		newVolumeErr: civogo.DatabaseVolumeDuplicateNameError,
	}

	d, _ := driver.NewTestDriver(nil)
	d.CivoClient = fc

	// We need ListVolumes to return the existing volume on the FIRST call
	// (the pre-check) only after we've already poisoned the volume into the
	// fake's storage. To reproduce the race more faithfully (pre-check sees
	// no match, NewVolume rejects with duplicate-name, idempotent lookup
	// finds it), we'd swap the storage between calls. For unit-testing the
	// lookup path it's enough that the volume exists when the lookup runs.

	resp, err := d.CreateVolume(context.Background(), minimalVolumeRequest("raced-vol"))
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "existing-vol-id", resp.Volume.VolumeId, "CSI must return the existing volume's id after a duplicate-name response")
}

// TestCreateVolume_DuplicateNameWithMissingVolumeReturnsError covers the rare
// case where api-go returns DatabaseVolumeDuplicateNameError but the volume
// isn't in our ListVolumes lookup (e.g. it was deleted between the API
// rejection and our lookup). The original duplicate-name error must be
// returned so external-provisioner can retry rather than silently succeeding.
func TestCreateVolume_DuplicateNameWithMissingVolumeReturnsError(t *testing.T) {
	base, _ := civogo.NewFakeClient()
	fc := &fakeWithHooks{
		FakeClient:   base,
		newVolumeErr: civogo.DatabaseVolumeDuplicateNameError,
	}

	d, _ := driver.NewTestDriver(nil)
	d.CivoClient = fc

	resp, err := d.CreateVolume(context.Background(), minimalVolumeRequest("ghost-vol"))
	assert.Nil(t, resp)
	if err == nil {
		t.Fatalf("expected an error")
	}
	// Confirm it's the original duplicate-name error wrapped, not a generic
	// internal error from the lookup.
	if !errors.Is(err, civogo.DatabaseVolumeDuplicateNameError) {
		t.Errorf("expected DatabaseVolumeDuplicateNameError, got %v (%T)", err, err)
	}
	// Sanity: NewVolume was called once, ListVolumes was called twice (the
	// pre-check + the idempotent lookup after the duplicate-name response).
	if got := atomic.LoadInt32(&fc.newVolumeCalls); got != 1 {
		t.Errorf("expected exactly 1 NewVolume call, got %d", got)
	}
	if got := atomic.LoadInt32(&fc.listVolumeCalls); got != 2 {
		t.Errorf("expected 2 ListVolumes calls (pre-check + lookup), got %d", got)
	}
}

// Sanity: compile-time assertion that fakeWithHooks satisfies civogo.Clienter
// so the test driver accepts it.
var _ civogo.Clienter = (*fakeWithHooks)(nil)

// Avoid unused-import warnings if the file is trimmed in future refactors.
var _ = fmt.Sprintf
