package handler

import (
	"context"
	"log"
	"strings"
	"sync"
)

const (
	defaultPressMaxInflight = 8
	// maxQueuedTasksPerDevice acota la bandeja de cada device. Si se llena se
	// descarta la tarea mas vieja que no sea un press.
	maxQueuedTasksPerDevice = 16
)

type deviceTaskKind int

const (
	deviceTaskPress deviceTaskKind = iota
	deviceTaskCaptureImage
	deviceTaskCaptureAudio
)

func (k deviceTaskKind) String() string {
	switch k {
	case deviceTaskPress:
		return "press"
	case deviceTaskCaptureImage:
		return "capture_image"
	case deviceTaskCaptureAudio:
		return "capture_audio"
	default:
		return "unknown"
	}
}

type deviceTask struct {
	kind    deviceTaskKind
	trackID string
	run     func(ctx context.Context) error
}

// deviceDispatcher saca el trabajo lento del callback de MQTT. El callback
// solo encola (microsegundos) y cada device tiene una bandeja FIFO con un
// unico worker, asi los mensajes de un mismo device se procesan en orden y
// los de devices distintos en paralelo. inflight limita cuantas tareas corren
// a la vez entre todos los devices.
//
// Reglas de encolado por device:
//   - press: gana el ultimo (reemplaza un press que todavia no arranco).
//   - capture_image: gana la ultima del mismo track (no se acumulan fotos
//     viejas mientras el analisis anterior esta en curso).
//   - capture_audio: se encolan todas, en orden.
type deviceDispatcher struct {
	inflight chan struct{}

	mu     sync.Mutex
	queues map[string]*deviceQueue
	closed bool
	wg     sync.WaitGroup
}

type deviceQueue struct {
	tasks   []deviceTask
	running bool
}

func newDeviceDispatcher(maxInflight int) *deviceDispatcher {
	if maxInflight <= 0 {
		maxInflight = defaultPressMaxInflight
	}
	return &deviceDispatcher{
		inflight: make(chan struct{}, maxInflight),
		queues:   make(map[string]*deviceQueue),
	}
}

// Submit encola la tarea en la bandeja del device. Nunca bloquea.
func (d *deviceDispatcher) Submit(deviceID string, task deviceTask) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || task.run == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}

	queue := d.queues[deviceID]
	if queue == nil {
		queue = &deviceQueue{}
		d.queues[deviceID] = queue
	}
	queue.enqueue(deviceID, task)

	if queue.running {
		return
	}
	queue.running = true
	d.wg.Add(1)
	go d.run(deviceID)
}

// DropTrack descarta las tareas encoladas (que todavia no arrancaron) de ese
// track. Se usa cuando llega stream_end: no tiene sentido procesar un press o
// un capture de una llamada que ya termino.
func (d *deviceDispatcher) DropTrack(deviceID, trackID string) int {
	deviceID = strings.TrimSpace(deviceID)
	trackID = strings.TrimSpace(trackID)
	if deviceID == "" || trackID == "" {
		return 0
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	queue := d.queues[deviceID]
	if queue == nil {
		return 0
	}

	kept := queue.tasks[:0]
	dropped := 0
	for _, task := range queue.tasks {
		if task.trackID == trackID {
			dropped++
			continue
		}
		kept = append(kept, task)
	}
	queue.tasks = kept
	return dropped
}

func (q *deviceQueue) enqueue(deviceID string, task deviceTask) {
	switch task.kind {
	case deviceTaskPress:
		for i, queued := range q.tasks {
			if queued.kind == deviceTaskPress {
				log.Printf("press pending replaced device=%s old=%s new=%s", deviceID, queued.trackID, task.trackID)
				q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
				break
			}
		}
	case deviceTaskCaptureImage:
		for i, queued := range q.tasks {
			if queued.kind == deviceTaskCaptureImage && queued.trackID == task.trackID {
				log.Printf("capture_image pending replaced by a newer one device=%s trackId=%s", deviceID, task.trackID)
				q.tasks[i] = task
				return
			}
		}
	}

	q.tasks = append(q.tasks, task)

	if len(q.tasks) <= maxQueuedTasksPerDevice {
		return
	}
	for i, queued := range q.tasks {
		if queued.kind != deviceTaskPress {
			log.Printf("device queue full device=%s, dropped oldest %s trackId=%s", deviceID, queued.kind, queued.trackID)
			q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
			return
		}
	}
}

func (d *deviceDispatcher) run(deviceID string) {
	defer d.wg.Done()

	for {
		d.mu.Lock()
		queue := d.queues[deviceID]
		if queue == nil || len(queue.tasks) == 0 {
			if queue != nil {
				queue.running = false
				delete(d.queues, deviceID)
			}
			d.mu.Unlock()
			return
		}
		task := queue.tasks[0]
		queue.tasks[0] = deviceTask{}
		queue.tasks = queue.tasks[1:]
		d.mu.Unlock()

		d.inflight <- struct{}{}
		func() {
			defer func() { <-d.inflight }()
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("device worker panic device=%s task=%s: %v", deviceID, task.kind, recovered)
				}
			}()
			if err := task.run(context.Background()); err != nil {
				log.Printf("MQTT PROCESSING ERROR device=%s trackId=%s task=%s: %v", deviceID, task.trackID, task.kind, err)
			}
		}()
	}
}

// Close deja de aceptar tareas y espera a que se vacien las bandejas.
func (d *deviceDispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.wg.Wait()
}
