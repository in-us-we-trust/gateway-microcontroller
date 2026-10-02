package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taskRecorder registra el orden de ejecucion y cuantas tareas corren a la vez.
type taskRecorder struct {
	mu          sync.Mutex
	order       []string
	inFlight    int
	maxInFlight int
}

func (r *taskRecorder) task(kind deviceTaskKind, trackID, label string, block <-chan struct{}, started chan<- struct{}) deviceTask {
	return deviceTask{
		kind:    kind,
		trackID: trackID,
		run: func(context.Context) error {
			r.mu.Lock()
			r.inFlight++
			if r.inFlight > r.maxInFlight {
				r.maxInFlight = r.inFlight
			}
			r.order = append(r.order, label)
			r.mu.Unlock()

			if started != nil {
				close(started)
			}
			if block != nil {
				<-block
			} else {
				time.Sleep(20 * time.Millisecond)
			}

			r.mu.Lock()
			r.inFlight--
			r.mu.Unlock()
			return nil
		},
	}
}

func (r *taskRecorder) snapshot() ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...), r.maxInFlight
}

// blockFirstTask arranca una tarea que no termina hasta cerrar el canal
// devuelto, para poder encolar detras de ella.
func blockFirstTask(t *testing.T, d *deviceDispatcher, rec *taskRecorder, deviceID string) chan struct{} {
	t.Helper()
	release := make(chan struct{})
	started := make(chan struct{})
	d.Submit(deviceID, rec.task(deviceTaskCaptureAudio, "track-0", "first", release, started))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first task did not start")
	}
	return release
}

func TestDeviceDispatcher_TwoDevicesRunInParallel(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	d.Submit("device-a", rec.task(deviceTaskPress, "track-a", "a", nil, nil))
	d.Submit("device-b", rec.task(deviceTaskPress, "track-b", "b", nil, nil))
	d.Close()

	order, max := rec.snapshot()
	assert.ElementsMatch(t, []string{"a", "b"}, order)
	assert.GreaterOrEqual(t, max, 2)
}

func TestDeviceDispatcher_SameDeviceRunsInOrderOneAtATime(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	release := blockFirstTask(t, d, rec, "device-a")

	d.Submit("device-a", rec.task(deviceTaskCaptureAudio, "track-1", "audio-1", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskCaptureImage, "track-1", "image-1", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskCaptureAudio, "track-1", "audio-2", nil, nil))
	close(release)
	d.Close()

	order, max := rec.snapshot()
	assert.Equal(t, []string{"first", "audio-1", "image-1", "audio-2"}, order)
	assert.Equal(t, 1, max)
}

func TestDeviceDispatcher_SameDeviceKeepsLatestPendingPress(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	release := blockFirstTask(t, d, rec, "device-a")

	d.Submit("device-a", rec.task(deviceTaskPress, "track-2", "press-2", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskPress, "track-3", "press-3", nil, nil))
	close(release)
	d.Close()

	order, _ := rec.snapshot()
	assert.Equal(t, []string{"first", "press-3"}, order)
}

func TestDeviceDispatcher_CaptureImageKeepsLatestPendingPerTrack(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	release := blockFirstTask(t, d, rec, "device-a")

	d.Submit("device-a", rec.task(deviceTaskCaptureImage, "track-1", "image-1", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskCaptureAudio, "track-1", "audio-1", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskCaptureImage, "track-1", "image-2", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskCaptureImage, "track-1", "image-3", nil, nil))
	close(release)
	d.Close()

	// La foto mas nueva ocupa el lugar de la pendiente; los audios no se pierden.
	order, _ := rec.snapshot()
	assert.Equal(t, []string{"first", "image-3", "audio-1"}, order)
}

func TestDeviceDispatcher_CaptureAudioIsNeverCoalesced(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	release := blockFirstTask(t, d, rec, "device-a")

	for _, label := range []string{"audio-1", "audio-2", "audio-3"} {
		d.Submit("device-a", rec.task(deviceTaskCaptureAudio, "track-1", label, nil, nil))
	}
	close(release)
	d.Close()

	order, _ := rec.snapshot()
	assert.Equal(t, []string{"first", "audio-1", "audio-2", "audio-3"}, order)
}

func TestDeviceDispatcher_DropTrackDiscardsQueuedTasksOfThatTrack(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	release := blockFirstTask(t, d, rec, "device-a")

	d.Submit("device-a", rec.task(deviceTaskCaptureAudio, "track-1", "audio-1", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskCaptureImage, "track-1", "image-1", nil, nil))
	d.Submit("device-a", rec.task(deviceTaskPress, "track-2", "press-2", nil, nil))

	assert.Equal(t, 2, d.DropTrack("device-a", "track-1"))
	assert.Equal(t, 0, d.DropTrack("device-b", "track-1"))
	close(release)
	d.Close()

	order, _ := rec.snapshot()
	assert.Equal(t, []string{"first", "press-2"}, order)
}

func TestDeviceDispatcher_QueueCapDropsOldestNonPress(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	release := blockFirstTask(t, d, rec, "device-a")

	d.Submit("device-a", rec.task(deviceTaskPress, "track-2", "press", nil, nil))
	for i := 0; i < maxQueuedTasksPerDevice; i++ {
		d.Submit("device-a", rec.task(deviceTaskCaptureAudio, "track-2", "audio", nil, nil))
	}
	close(release)
	d.Close()

	order, _ := rec.snapshot()
	require.Len(t, order, 1+maxQueuedTasksPerDevice)
	assert.Equal(t, "press", order[1], "press is never dropped by the cap")
}

func TestDeviceDispatcher_GlobalInflightCap(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(1)
	d.Submit("device-a", rec.task(deviceTaskPress, "track-a", "a", nil, nil))
	d.Submit("device-b", rec.task(deviceTaskPress, "track-b", "b", nil, nil))
	d.Close()

	_, max := rec.snapshot()
	assert.Equal(t, 1, max)
}

func TestDeviceDispatcher_SubmitNeverBlocks(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(1)
	release := blockFirstTask(t, d, rec, "device-a")

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			d.Submit("device-a", rec.task(deviceTaskCaptureAudio, "track-1", "audio", nil, nil))
			d.Submit("device-b", rec.task(deviceTaskCaptureAudio, "track-1", "audio", nil, nil))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Submit blocked while a task was running")
	}
	close(release)
	d.Close()
}

func TestDeviceDispatcher_IgnoresEmptyDeviceAndSubmitAfterClose(t *testing.T) {
	t.Parallel()

	rec := &taskRecorder{}
	d := newDeviceDispatcher(8)
	d.Submit("  ", rec.task(deviceTaskPress, "track-a", "empty", nil, nil))
	d.Close()
	d.Submit("device-a", rec.task(deviceTaskPress, "track-a", "late", nil, nil))

	order, _ := rec.snapshot()
	assert.Empty(t, order)
}
