# Secure Home Edge Gateway

`gateway-microcontroller` is the edge gateway for the Secure Home smart doorbell system. It keeps the existing Gin HTTP API and adds an MQTT control plane plus an RTSP media plane.

When an ESP32 publishes a doorbell press, the gateway:

1. Reads `deviceId` and `trackId` (`eventId`) from the MQTT topic `home/{deviceId}/events/{trackId}/press`.
2. Stores an active `deviceId -> trackId` session with a configurable TTL.
3. Builds the RTSP URL as `{RTSP_BASE}/{deviceId}/doorbell/{trackId}` (the press JSON does not carry `rtspUrl`).
4. If the press carries a JPEG in the MQTT `image` field (Base64), forwards it as `IMAGE` in the background. A missing, `null`, empty or invalid `image` means no snapshot (there is no RTSP fallback).
5. Starts pulling short video and audio chunks from that RTSP path right away, without waiting for the snapshot result (does not buffer the whole stream).
6. Sends each chunk to Detective Manager with the same `trackId`.
7. If `DEBUG_MEDIA_ENABLED` is true, writes sent files under `DEBUG_MEDIA_DIR` (capped by `DEBUG_MEDIA_MAX_FILES`).
8. Stops processing when a stream-end message arrives, the session TTL expires, or Detective Manager returns `STOP_DANGER`.

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

The gateway subscribes only to the actions the ESP32 publishes: `home/+/events/+/press`, `stream_end`, `capture_image` and `capture_audio` (not `home/+/events/#`, so it does not receive its own `ia_response`). Each call lives under its own `deviceId` and `trackId` (`eventId` of the press). Status and telemetry, if wired later, live at `home/{deviceId}/status` and `home/{deviceId}/telemetry` (not under a `trackId`).

| Topic                                            | Direction        | QoS | Current behavior                                                |
| ------------------------------------------------ | ---------------- | --: | --------------------------------------------------------------- |
| `home/{deviceId}/events/{trackId}/press`         | ESP32 to gateway |   1 | Starts a session and media workers                              |
| `home/{deviceId}/events/{trackId}/stream_end`    | ESP32 to gateway |   1 | Stops that session if `trackId` matches                         |
| `home/{deviceId}/events/{trackId}/capture_image` | ESP32 to gateway |   1 | One-shot JPEG from the session or built RTSP URL                |
| `home/{deviceId}/events/{trackId}/capture_audio` | ESP32 to gateway |   1 | One-shot audio from the session or built RTSP URL               |
| `home/{deviceId}/status`                         | ESP32 to gateway |   1 | Defined, not subscribed yet                                     |
| `home/{deviceId}/telemetry`                      | ESP32 to gateway |   0 | Defined, not subscribed yet                                     |
| `home/{deviceId}/events/{trackId}/ia_response`   | Gateway to ESP32 |   1 | Compact CONTINUE / STOP_DANGER after each orchestrator response |

Each `ia_response` includes a unique `messageId`. The same `messageId` twice means MQTT redelivery, not two gateway publishes.

Doorbell press payload (`image` and `batteryPercentage` optional):

```json
{
  "timestamp": 1776268805120,
  "event": "press",
  "image": "/9j/4AAQSkZJRgABAQAAAQABAAD...",
  "batteryPercentage": 94.5
}
```

The ESP32 generates `eventId` (`trackId`) **before** the press and publishes RTSP to `{RTSP_BASE}/{deviceId}/doorbell/{trackId}` first. `image` is a JPEG snapshot only; full video stays on RTSP. Sending the same active `eventId` again is treated as a duplicate and does not start another pair of media workers.

Recommended stream-end payload:

```json
{
  "timestamp": 1776268835120
}
```

`trackId` in the topic is required. A delayed stream-end for an older `trackId` does not close a newer session for the same device.

## Detective Manager Contract

Every media artifact is sent to:

```text
POST {DETECTIVE_MANAGER_URL}/api/detective-manager/v1/orchestrator/events
Content-Type: multipart/form-data
```

Multipart fields:

| Field       | Value                        |
| ----------- | ---------------------------- |
| `trackId`   | Doorbell press `eventId`     |
| `deviceId`  | Physical device UUID         |
| `mediaType` | `IMAGE`, `AUDIO`, or `VIDEO` |
| `file`      | JPEG, WAV/MP3, or MP4 binary |

File MIME types are:

- JPEG: `image/jpeg`
- MP3: `audio/mpeg`
- WAV: `audio/wav`
- MP4: `video/mp4`

Response:

```json
{
  "data": {
    "trackId": "string",
    "chunkType": "IMAGE",
    "finalVerdict": "STOP_DANGER",
    "finalSeverity": "PELIGROSO",
    "message": "No se reconoce a la persona"
  }
}
```

## Prerequisites

For local development:

- Go 1.22 or newer
- FFmpeg available on `PATH`
- An MQTT broker
- An RTSP source containing video and audio
- A Detective Manager instance or an HTTP mock

For the Docker workflow:

- Docker Desktop (optional; `docker build` / `docker run`, CD, and `.\dev.ps1 up` use the Dockerfile)
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
# set DETECTIVE_MANAGER_URL to the real service, MQTT_BROKER_URL / RTSP_BASE to the VM
go run ./cmd/api
# or docker (see below) / .\dev.ps1 up
```

### 2. Simulate the ESP32 (OBS + press)

```powershell
.\dev.ps1 obs
# Start streaming in OBS to rtmp://<MQTT_HOST>:1935/{deviceId}/doorbell/{trackId}
.\dev.ps1 press
.\dev.ps1 end
```

Optional file camera (instead of OBS):

```powershell
.\dev.ps1 rtsp -File ".\gorras frente.mp4"
```

The gateway builds `rtsp://<RTSP_BASE host>/{deviceId}/doorbell/{trackId}`. Set `RTSP_BASE` to an address the gateway process can reach (the VM IP from Docker, `localhost` from the host). The press payload no longer includes `rtspUrl`.

Expected after `press`:

1. The gateway creates the session.
2. One `IMAGE` request is sent only if the press carried `image` (MQTT snapshot).
3. `VIDEO` and `AUDIO` chunks start in parallel on the configured windows, without waiting for the `IMAGE` response.
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
RTSP_BASE=rtsp://<MQTT_HOST>:8554
```

Against the secure Mosquitto/MediaMTX profile (client certs in `./certs`, password of the **gateway** user in the RTSP URL):

```dotenv
MQTT_BROKER_URL=ssl://<MQTT_HOST>:8883
MQTT_CA_CERT=/certs/ca.crt
MQTT_CLIENT_CERT=/certs/gateway.crt
MQTT_CLIENT_KEY=/certs/gateway.key
RTSP_BASE=rtsps://gateway:<gateway-rtsp-password>@<MQTT_HOST>:8322
RTSP_CA_CERT=/certs/ca.crt
```

```powershell
go run ./cmd/api
```

Or the same image Cloud Run uses (no Compose):

```powershell
docker build -t gateway-microcontroller:local .
docker rm -f gateway-microcontroller 2>$null
docker run --rm --name gateway-microcontroller `
  --env-file .env `
  -p 8080:8080 `
  -v "${PWD}/debug-chunks:/app/debug-chunks" `
  -v "${PWD}/certs:/certs:ro" `
  gateway-microcontroller:local
```

Logs (follow):

```powershell
docker logs -f gateway-microcontroller
```

If you ran `docker run` in the foreground, logs are already in that terminal.

Stop and remove it:

```powershell
docker rm -f gateway-microcontroller
```

If you ran `docker run` in the foreground, Ctrl+C is enough (`--rm` deletes the container on exit). `.\dev.ps1 down` does the same `rm -f` plus the publisher container.

From inside the container, `127.0.0.1` is the container itself. Point `MQTT_BROKER_URL` and `RTSP_BASE` at the VM (or `host.docker.internal` if Mosquitto/MediaMTX are on the Docker host). `.\dev.ps1 up` is this build + `docker run -d`.

Publish RTSP to `{RTSP_BASE}/{deviceId}/doorbell/{trackId}` before `.\dev.ps1 press`.

## Environment Variables

| Variable                 | Default                     | Description                                                                                                                                                                 |
| ------------------------ | --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PORT`                   | `8080`                      | Gin HTTP server port                                                                                                                                                        |
| `DETECTIVE_MANAGER_URL`  | `http://localhost:8081`     | Detective Manager base URL                                                                                                                                                  |
| `ENABLE_MTLS`            | `true`                      | Uses the client-certificate header for legacy HTTP routes                                                                                                                   |
| `MQTT_ENABLED`           | `true`                      | Enables MQTT connection and subscriptions                                                                                                                                   |
| `MQTT_BROKER_URL`        | `tcp://localhost:1883`      | MQTT broker URL (`ssl://host:8883` or `mqtts://` for MQTTS)                                                                                                                 |
| `MQTT_CLIENT_ID`         | `gateway-micro`             | MQTT client identifier; it must be unique per gateway. CN of the client cert should match this when using Mosquitto `use_identity_as_username`                              |
| `MQTT_USERNAME`          | empty                       | MQTT username (optional if the broker takes identity from the client cert)                                                                                                  |
| `MQTT_PASSWORD`          | empty                       | MQTT password                                                                                                                                                               |
| `MQTT_CA_CERT`           | empty                       | CA PEM or file path used to verify the broker. Required for self-signed MQTTS                                                                                               |
| `MQTT_CLIENT_CERT`       | empty                       | Gateway client cert PEM or file path (CN `gateway-micro`)                                                                                                                   |
| `MQTT_CLIENT_KEY`        | empty                       | Gateway client key PEM or file path. Must be set together with `MQTT_CLIENT_CERT`                                                                                           |
| `MQTT_TLS_INSECURE`      | `false`                     | Skip broker certificate verification (lab only)                                                                                                                             |
| `SESSION_TTL`            | `30s`                       | Maximum session duration; it is not refreshed by media activity                                                                                                             |
| `AUDIO_CHUNK_DURATION`   | `10s`                       | Requested length of each audio chunk                                                                                                                                        |
| `VIDEO_CHUNK_DURATION`   | `5s`                        | Requested length of each video chunk                                                                                                                                        |
| `AUDIO_FORMAT`           | `mp3`                       | Output format: `mp3` or `wav`                                                                                                                                               |
| `MEDIA_CHUNK_BATCH_SIZE` | `1`                         | Kept for compatibility; it no longer serializes VIDEO behind AUDIO. Each type has its own worker                                                                            |
| `DEBUG_MEDIA_ENABLED`    | unset                       | If unset or false, no debug files are written                                                                                                                               |
| `DEBUG_MEDIA_MAX_FILES`  | `20` when enabled           | Maximum files kept in the debug directory                                                                                                                                   |
| `DEBUG_MEDIA_DIR`        | `debug-chunks` when enabled | Directory for those files                                                                                                                                                   |
| `FFMPEG_PATH`            | `ffmpeg`                    | FFmpeg executable path                                                                                                                                                      |
| `RTSP_BASE`              | `rtsp://localhost:8554`     | MediaMTX base used to build `{RTSP_BASE}/{deviceId}/doorbell/{trackId}`. Include gateway user/password when MediaMTX requires auth, e.g. `rtsps://gateway:gw-lab@host:8322` |
| `RTSP_CA_CERT`           | empty                       | CA file path passed to FFmpeg (`-ca_file`) for RTSPS                                                                                                                        |
| `RTSP_TLS_INSECURE`      | `false`                     | FFmpeg `-tls_verify 0` for RTSPS (lab / self-signed)                                                                                                                        |
| `MEDIA_REQUEST_TIMEOUT`  | `15s`                       | Media extraction and outbound HTTP timeout (also `http.Client` timeout)                                                                                                     |
| `PRESS_MAX_INFLIGHT`     | `8`                         | Max MQTT tasks (press setup, `capture_image`, `capture_audio`) running at once across all devices. Each device processes its own tasks one at a time, in order |

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

- The MQTT callback never does I/O. `press`, `capture_image` and `capture_audio` go to a per-`deviceId` queue with one worker per device (in order, one at a time); different devices run in parallel under the global `PRESS_MAX_INFLIGHT` cap. Queue rules: latest pending `press` wins, latest pending `capture_image` of the same track wins, every `capture_audio` is kept (up to 16 queued tasks per device, oldest non-press dropped beyond that).
- `stream_end` runs in the callback (it only cancels the session) and drops the queued tasks of that track. A capture of the active call is bound to the session context, so `stream_end`, `STOP_DANGER` or the TTL cut its FFmpeg and POST.
- After a valid press, the optional IMAGE snapshot and the VIDEO and AUDIO workers start in parallel. A `STOP_DANGER` on the IMAGE ends the session and stops the workers.
- Within each worker, chunks are extracted and dispatched sequentially (extract then POST). A slow AUDIO POST does not block VIDEO extraction.
- The extractor retries after errors, but failed Detective Manager HTTP requests are logged and the affected media artifact is not retried.
- There is no durable queue or disk spool. A process crash or network outage can lose media events.
- Multipart request bodies are buffered in memory before transmission. The configured short audio windows keep buffers bounded, but concurrency and chunk size still affect memory usage.
- Multipart bodies are intentionally omitted from outgoing request logs to avoid logging binary media.

### Deployment

- This service is designed for an edge host that can reach MediaMTX (`RTSP_BASE`).
- A public cloud runtime usually cannot reach a private LAN RTSP URL without a VPN or private network path.
- The development Mosquitto in `microcontroller-communication` allows anonymous connections. Point this gateway at `tcp://` / `rtsp://` for that lab profile. The secure profile uses `ssl://host:8883` plus `MQTT_CA_CERT` / `MQTT_CLIENT_CERT` / `MQTT_CLIENT_KEY` (CN `gateway-micro`) and `rtsps://gateway:<password>@host:8322`. Mount cert files at `/certs` (Compose maps `./certs`). The gateway does not look up per-device RTSP passwords; those belong to MediaMTX and the publisher (ESP32 / OBS).
- FFmpeg is installed in the Docker image; native deployments must install it separately.
- CPU and memory requirements grow with the number of simultaneous doorbell sessions.

### Current scope

- The original Gin HTTP API remains available for backward compatibility.
- MQTT status and telemetry ingestion are not implemented.
- AI responses are published on `home/{deviceId}/events/{trackId}/ia_response`.
- Press MQTT carries a JPEG snapshot. Video and audio continue over RTSP as incremental chunks. The ESP32 does not send `rtspUrl`; the gateway builds it from `RTSP_BASE`.

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

- Confirm that the RTSP URL built from `RTSP_BASE` is reachable from the gateway process or container.
- Confirm that the stream has a video track.
- Increase `MEDIA_REQUEST_TIMEOUT`.
- When using Docker Desktop, replace `localhost` with `host.docker.internal`.

### Audio extraction repeatedly fails

Run:

```powershell
ffprobe "rtsp://localhost:8554/{deviceId}/doorbell/{trackId}"
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
