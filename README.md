# Secure Home Edge Gateway

`gateway-microcontroller` is the edge gateway for the Secure Home smart doorbell system. It keeps the existing Gin HTTP API and adds an MQTT control plane plus an RTSP media plane.

When an ESP32 publishes a doorbell press, the gateway:

1. Uses the MQTT `eventId` as the session `trackId`.
2. Stores an active `deviceId -> trackId` session with a configurable TTL.
3. Forwards the press JPEG snapshot from the MQTT `image` field (Base64) as `IMAGE`.
4. Pulls short video and audio chunks from RTSP incrementally (does not buffer the whole stream).
5. Sends each chunk to Detective Manager with the same `trackId`.
6. If `DEBUG_MEDIA_ENABLED` is true, writes sent files under `DEBUG_MEDIA_DIR` (capped by `DEBUG_MEDIA_MAX_FILES`).
7. Stops processing when a stream-end message arrives, the session TTL expires, or Detective Manager returns `STOP_DANGER`.

## Architecture

```text
ESP32
  |
  | MQTT: press / stream_end
  v
EventHandler
  |
  v
EventService ------> SessionRegistry
  |                     |
  |                     +-- deviceId -> trackId, RTSP URL, expiration
  |
  +--> FFmpegMediaExtractor --> RTSP video/audio
  |
  +--> DetectorClient --> multipart/form-data --> Detective Manager
```

The main implementation is organized as follows:

```text
cmd/api/main.go                          Application wiring
internal/domain/event.go                 MQTT and media event contracts
internal/domain/session.go               Active session state
internal/handler/event_handler.go        MQTT subscriptions and HTTP event handler
internal/service/session_service.go      Thread-safe session registry and TTL
internal/service/event_service.go        Snapshot + video/audio chunk orchestration
internal/service/debug_media.go          Optional local copies of sent chunks
internal/service/media_extractor.go      FFmpeg adapter
internal/client/http_detector_client.go  Detective Manager HTTP client
```

## MQTT Contracts

| Topic | Direction | QoS | Current behavior |
|---|---|---:|---|
| `home/doorbell/events/press` | ESP32 to gateway | 1 | Starts a session and media workers |
| `home/doorbell/events/stream_end` | ESP32 to gateway | 1 | Stops the active session |
| `home/doorbell/status` | ESP32 to gateway | 1 | Defined, not subscribed yet |
| `home/doorbell/telemetry` | ESP32 to gateway | 0 | Defined, not subscribed yet |
| `home/doorbell/events/ia_response` | Gateway to ESP32 | 1 | Compact CONTINUE / STOP_DANGER after each orchestrator response |

Doorbell press payload:

```json
{
  "eventId": "dddddddd-ffff-dddd-dddd-ddddddddddaa",
  "deviceId": "802fbc00-e78a-46fb-940f-4f462853b6cb",
  "timestamp": 1776268805120,
  "event": "press",
  "image": "/9j/4AAQSkZJRgABAQAAAQABAAD...",
  "rtspUrl": "rtsp://192.168.1.50:554/stream/v1",
  "batteryPercentage": 94.5
}
```

`eventId` becomes the session `trackId`. `image` is a JPEG snapshot only; full video stays on RTSP. Sending the same active `eventId` again is treated as a duplicate and does not start another pair of media workers.

Recommended stream-end payload:

```json
{
  "deviceId": "802fbc00-e78a-46fb-940f-4f462853b6cb",
  "trackId": "dddddddd-ffff-dddd-dddd-ddddddddddaa",
  "timestamp": 1776268835120
}
```

`trackId` is optional for compatibility. It is recommended because it prevents a delayed stream-end event from closing a newer session for the same device.

## Detective Manager Contract

Every media artifact is sent to:

```text
POST {DETECTIVE_MANAGER_URL}/api/detective-manager/v1/orchestrator/events
Content-Type: multipart/form-data
```

Multipart fields:

| Field | Value |
|---|---|
| `trackId` | Doorbell press `eventId` |
| `deviceId` | Physical device UUID |
| `mediaType` | `IMAGE`, `AUDIO`, or `VIDEO` |
| `file` | JPEG, WAV/MP3, or MP4 binary |

File MIME types are:

- JPEG: `image/jpeg`
- MP3: `audio/mpeg`
- WAV: `audio/wav`
- MP4: `video/mp4`

## Prerequisites

For local development:

- Go 1.22 or newer
- FFmpeg available on `PATH`
- An MQTT broker
- An RTSP source containing video and audio
- A Detective Manager instance or an HTTP mock

For the Docker workflow:

- Docker Desktop (optional; CD and `.\dev.ps1 up` use the Dockerfile)
- A reachable ESP32 or OBS RTSP stream

The application image installs FFmpeg automatically. A native execution requires a local FFmpeg installation:

```powershell
ffmpeg -version
go version
docker version
```

## Automated Tests

Run the complete test suite with Go 1.22 or newer:

```powershell
go test ./...
```

Run formatting and static checks:

```powershell
gofmt -l .
go vet ./...
```

Run the multipart client tests only:

```powershell
go test ./internal/client -run TestHTTPDetectorClient_SendMediaEvent_SendsMultipartPayload -v
```

These tests use `httptest.Server` and verify:

- The orchestrator endpoint and HTTP method.
- `trackId`, `deviceId`, and `mediaType`.
- JPEG and MP3 file buffers.
- `image/jpeg` and `audio/mpeg` multipart headers.

Run session and media orchestration tests:

```powershell
go test ./internal/service -v
```

These tests verify:

- Session creation, lookup, explicit closure, and TTL expiration.
- Safe replacement of a device session.
- Reuse of one `trackId` for IMAGE and AUDIO events.
- Media processing without requiring a real RTSP stream or FFmpeg process.

Run the legacy HTTP integration tests:

```powershell
go test ./tests/integration/... -v
```

The legacy tests keep the existing HTTP event and device-registration paths backward compatible.

> The automated tests mock the media extractor. The end-to-end procedure below is required to validate FFmpeg, RTSP connectivity, MQTT, and the real multipart network path together.

## End-to-End Test

MQTT (Mosquitto) and RTSP (MediaMTX) are **not** in this repo. Start them from `microcontroller-communication` first. See `DESACOPLE.md`.

There is no Docker Compose here. The gateway is one process (Cloud Run in CD, `go run` or `docker run` locally). ESP32 simulation lives in `dev.ps1` (press + OBS).

### 1. Start infra, then the gateway

```powershell
# in microcontroller-communication
docker compose up -d

# in this repo
Copy-Item .env.example .env
# set DETECTIVE_MANAGER_URL to the real service, MQTT_BROKER_URL to the VM broker
go run ./cmd/api
# or: .\dev.ps1 up
```

### 2. Simulate the ESP32 (OBS + press)

```powershell
.\dev.ps1 obs
# Start streaming in OBS to rtmp://<MQTT_HOST>:1935/doorbell
.\dev.ps1 press
.\dev.ps1 end
```

Optional file camera (instead of OBS):

```powershell
.\dev.ps1 rtsp -File ".\gorras frente.mp4"
```

When the gateway runs in Docker, the press payload must use an RTSP URL reachable from that container (the VM IP, not `localhost`). When it runs on the host, `rtsp://localhost:8554/doorbell` is fine if MediaMTX is local.

Expected after `press`:

1. The gateway creates the session.
2. One `IMAGE` request is sent (MQTT snapshot).
3. `VIDEO` then `AUDIO` chunks follow the configured windows.
4. Every request contains the same `trackId`.

```powershell
.\dev.ps1 inspect   # optional debug files if DEBUG_MEDIA_ENABLED=true
.\dev.ps1 down      # stream_end + stop local gateway/publisher containers
```

Mosquitto and MediaMTX stay up in `microcontroller-communication`.

## Running the Gateway Directly

MQTT/RTSP stay in `microcontroller-communication`. Detective Manager is the real service URL in `.env`.

```powershell
Copy-Item ".env.example" ".env"
```

```dotenv
DETECTIVE_MANAGER_URL=https://your-detective-manager
MQTT_BROKER_URL=tcp://<MQTT_HOST>:1883
ENABLE_MTLS=false
FFMPEG_PATH=ffmpeg
```

```powershell
go run ./cmd/api
```

Use `rtsp://<MQTT_HOST>:8554/doorbell` in the MQTT press payload (or `.\dev.ps1 press`).

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Gin HTTP server port |
| `DETECTIVE_MANAGER_URL` | `http://localhost:8081` | Detective Manager base URL |
| `ENABLE_MTLS` | `true` | Uses the client-certificate header for legacy HTTP routes |
| `MQTT_ENABLED` | `true` | Enables MQTT connection and subscriptions |
| `MQTT_BROKER_URL` | `tcp://localhost:1883` | MQTT broker URL |
| `MQTT_CLIENT_ID` | `gateway-micro` | MQTT client identifier; it must be unique per gateway |
| `MQTT_USERNAME` | empty | MQTT username |
| `MQTT_PASSWORD` | empty | MQTT password |
| `SESSION_TTL` | `30s` | Maximum session duration; it is not refreshed by media activity |
| `AUDIO_CHUNK_DURATION` | `10s` | Requested length of each audio chunk |
| `VIDEO_CHUNK_DURATION` | `5s` | Requested length of each video chunk |
| `AUDIO_FORMAT` | `mp3` | Output format: `mp3` or `wav` |
| `MEDIA_CHUNK_BATCH_SIZE` | `1` | Extract N chunks, send them, then extract the next N |
| `DEBUG_MEDIA_ENABLED` | unset | If unset or false, no debug files are written |
| `DEBUG_MEDIA_MAX_FILES` | `20` when enabled | Maximum files kept in the debug directory |
| `DEBUG_MEDIA_DIR` | `debug-chunks` when enabled | Directory for those files |
| `FFMPEG_PATH` | `ffmpeg` | FFmpeg executable path |
| `MEDIA_REQUEST_TIMEOUT` | `10s` | Image extraction and outbound HTTP timeout |

`DETECTOR_SERVICE_URL` is still accepted as a fallback for backward compatibility, but new deployments should use `DETECTIVE_MANAGER_URL`.

## Implementation Considerations

### Session lifecycle

- Sessions are stored in memory and indexed by `deviceId`.
- A process restart loses all active sessions.
- The registry is appropriate for one gateway instance. Multiple replicas need shared state, such as Redis, plus ownership coordination for media workers.
- Starting a new session for a device cancels its previous session.
- TTL starts at the press event and is not extended by audio activity.
- A stream-end message with a non-matching `trackId` does not close the current session.

### FFmpeg and RTSP

- RTSP and RTSPS are the only accepted URL schemes.
- Snapshot extraction and audio extraction run as separate FFmpeg processes.
- Each audio chunk starts a new FFmpeg process and reconnects to RTSP. This is simple and robust for the first implementation, but it adds connection and CPU overhead.
- Audio is normalized to mono, 16 kHz.
- MP3 uses `libmp3lame` at 64 kbit/s; WAV uses signed 16-bit PCM.
- G.711, PCM, and AAC inputs are supported when the installed FFmpeg build includes the required decoder.
- A stream without audio can still produce an image, but the audio worker logs extraction errors and retries once per second.

### Concurrency and delivery

- Image and audio workers start asynchronously after a valid press event.
- Audio chunks are extracted and dispatched sequentially per session.
- The extractor retries after errors, but failed Detective Manager HTTP requests are logged and the affected media artifact is not retried.
- There is no durable queue or disk spool. A process crash or network outage can lose media events.
- Multipart request bodies are buffered in memory before transmission. The configured short audio windows keep buffers bounded, but concurrency and chunk size still affect memory usage.
- Multipart bodies are intentionally omitted from outgoing request logs to avoid logging binary media.

### Deployment

- This service is designed for an edge host that can reach the ESP32 RTSP address.
- A public cloud runtime usually cannot reach a private LAN RTSP URL without a VPN or private network path.
- The development Mosquitto in `microcontroller-communication` allows anonymous connections. Production must use authentication, TLS, network restrictions, and unique MQTT client IDs.
- FFmpeg is installed in the Docker image; native deployments must install it separately.
- CPU and memory requirements grow with the number of simultaneous doorbell sessions.

### Current scope

- The original Gin HTTP API remains available for backward compatibility.
- MQTT status and telemetry ingestion are not implemented.
- AI responses are published on `home/doorbell/events/ia_response`.
- Press MQTT carries a JPEG snapshot. Video and audio continue over RTSP as incremental chunks.

## Troubleshooting

### Gateway cannot connect to MQTT

Confirm that Mosquitto is running in `microcontroller-communication` and reachable:

```powershell
Test-NetConnection localhost -Port 1883
```

### FFmpeg executable is missing

Set an absolute path:

```dotenv
FFMPEG_PATH=C:\ffmpeg\bin\ffmpeg.exe
```

### Image extraction times out

- Confirm that the RTSP URL is reachable from the gateway process or container.
- Confirm that the stream has a video track.
- Increase `MEDIA_REQUEST_TIMEOUT`.
- When using Docker Desktop, replace `localhost` with `host.docker.internal`.

### Audio extraction repeatedly fails

Run:

```powershell
ffprobe "rtsp://localhost:8554/doorbell"
```

If FFmpeg reports `Stream map '0:a:0' matches no streams`, the RTSP source has no audio track.

### Detective Manager returns 404

The required path is:

```text
/api/detective-manager/v1/orchestrator/events
```

Verify `DETECTIVE_MANAGER_URL` points at the real Detective Manager.

### No audio request appears immediately

The first video request is sent after a complete `VIDEO_CHUNK_DURATION` window (5s). The first audio request follows a complete `AUDIO_CHUNK_DURATION` window (10s). The loop then repeats until `stream_end`, TTL, or `STOP_DANGER`.

## Swagger

The Swagger documentation covers the legacy HTTP API:

```text
http://localhost:8080/swagger/index.html
```

Regenerate it with:

```powershell
go run scripts.go swagger
```
