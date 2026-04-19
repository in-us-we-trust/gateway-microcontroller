# Secure Home Gateway

IoT gateway microservice for the **Secure Home** project. Receives security events from microcontrollers (motion sensors, doorbells) and propagates them to a downstream detection service that determines whether an intruder is present.

---

## Architecture — Layer Interaction

```
[IoT microcontroller]
        │  POST /api/v1/events
        │  Authorization: Bearer <token>
        ▼
┌───────────────────────┐
│   Middleware (auth)   │  pkg: internal/middleware
│                       │  Validates presence of Bearer token.
│                       │  TODO: validate JWT signature & claims.
└──────────┬────────────┘
           │ passes only if token header is present
           ▼
┌───────────────────────┐
│   Handler             │  pkg: internal/handler
│                       │  Parses & validates request body (type, image).
│                       │  Returns 400 with RFC 7807 error on invalid input.
└──────────┬────────────┘
           │ calls service with domain.EventRequest
           ▼
┌───────────────────────┐
│   Service             │  pkg: internal/service
│                       │  Orchestrates business logic.
│                       │  Calls DetectorClient interface — never the HTTP
│                       │  implementation directly.
└──────────┬────────────┘
           │ calls DetectorClient.Analyze(ctx, req)
           ▼
┌───────────────────────┐
│   Client              │  pkg: internal/client
│                       │  HTTPDetectorClient: propagates the same payload
│                       │  to DETECTOR_SERVICE_URL/analyze via HTTP POST.
└──────────┬────────────┘
           │ HTTP
           ▼
  [Detector Microservice]
  (mocked in docker-compose via WireMock)
```

### Dependency rule

Each layer depends only on the layer immediately below it, always through an **interface**:

| Layer | Depends on interface | Concrete impl lives in |
|---|---|---|
| Handler | `service.EventService` | `internal/service` |
| Service | `client.DetectorClient` | `internal/client` |
| Client | `*http.Client` (stdlib) | `internal/client` |

This means **tests can swap any layer** by providing a mock that satisfies the interface, without touching HTTP or I/O.

---

## Project Layout

```
gateway-microcontroller/
├── cmd/
│   └── api/
│       └── main.go              # Entrypoint: wires dependencies, starts Gin
├── docs/
│   └── docs.go                  # Embedded Swagger spec (regenerate with make swagger)
├── internal/
│   ├── client/
│   │   ├── detector_client.go   # DetectorClient interface
│   │   └── http_detector_client.go  # HTTP implementation
│   ├── domain/
│   │   └── event.go             # Request/response types and EventType enum
│   ├── handler/
│   │   └── event_handler.go     # HTTP handler + Swagger annotations
│   ├── middleware/
│   │   └── auth.go              # JWT Bearer enforcement
│   └── service/
│       └── event_service.go     # EventService interface + implementation
├── pkg/
│   └── apierror/
│       └── error.go             # RFC 7807 error struct and helpers
├── tests/
│   └── integration/
│       └── event_test.go        # Integration tests (only DetectorClient mocked)
├── Dockerfile                   # Multi-stage build
├── docker-compose.yml           # gateway + WireMock as detector stub
└── Makefile                     # Common dev commands
```

---

## API

### `POST /api/v1/events`

Reports a security event from a microcontroller.

**Headers**

| Name | Required | Description |
|---|---|---|
| `Authorization` | yes | `Bearer <jwt-token>` |
| `Content-Type` | yes | `application/json` |

**Request body**

```json
{
  "type":  "motion",
  "image": "<base64-encoded image>"
}
```

| Field | Type | Constraints |
|---|---|---|
| `type` | `string` | required — `"bell"` or `"motion"` |
| `image` | `string` | required — base64-encoded capture |

**Success response — 200**

```json
{
  "data": {
    "is_intruder": true
  }
}
```

**Error response (all 4xx/5xx)**

```json
{
  "status":   401,
  "title":    "Unauthorized",
  "detail":   "Authorization header with Bearer token is required",
  "instance": "/api/v1/events",
  "type":     "about:blank"
}
```

---

## Running Locally

### Prerequisites

- Go 1.18+
- Docker & Docker Compose (optional)

No other tools required — commands are driven by `scripts.go`, a plain Go file
that runs on Windows, macOS and Linux without any extra installation.

### Without Docker

```
# Install dependencies
go mod tidy

# Start the server (defaults: PORT=8080, DETECTOR_SERVICE_URL=http://localhost:8081)
go run scripts.go run

# Or with custom env vars (Windows PowerShell)
$env:PORT="9090"; $env:DETECTOR_SERVICE_URL="http://my-detector:8081"; go run ./cmd/api

# Or with custom env vars (macOS / Linux)
PORT=9090 DETECTOR_SERVICE_URL=http://my-detector:8081 go run ./cmd/api
```

### With Docker Compose

Starts the gateway and a [WireMock](https://wiremock.org/) instance as the detector stub:

```
docker-compose up --build
```

**Register the detector stub** (in a separate terminal after `up`):

```bash
curl -X POST http://localhost:8081/__admin/mappings \
  -H "Content-Type: application/json" \
  -d '{"request":{"method":"POST","url":"/analyze"},"response":{"status":200,"headers":{"Content-Type":"application/json"},"body":"{\"is_intruder\":true}"}}'
```

**Call the gateway:**

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"type":"motion","image":"abc123"}'
```

**WireMock admin endpoints:**

| URL | Description |
|---|---|
| `GET http://localhost:8081/__admin/mappings` | List registered stubs |
| `DELETE http://localhost:8081/__admin/mappings` | Clear all stubs |
| `GET http://localhost:8081/__admin/requests` | Inspect received requests |

---

## Running Tests

```
go run scripts.go test
```

Tests are **integration tests** — they wire the real Handler → Middleware → Service chain and only mock `DetectorClient` (the external HTTP boundary).

---

## Swagger UI

Start the server and open:

```
http://localhost:8080/swagger/index.html
```

To regenerate the spec from source annotations (requires [swag CLI](https://github.com/swaggo/swag)):

```
go run scripts.go swagger
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Port the gateway listens on |
| `DETECTOR_SERVICE_URL` | `http://localhost:8081` | Base URL of the downstream detection service |

---

## Extending the Project

- **Add a new event type**: add the constant to `domain.EventType` and update the `oneof` binding tag in `domain.EventRequest`.
- **Add a new endpoint**: create a handler in `internal/handler`, define a service interface in `internal/service`, register the route in `cmd/api/main.go`.
- **Implement real JWT validation**: replace the `TODO` in `internal/middleware/auth.go` — parse and verify the token against a trusted JWKS endpoint without breaking the interface contract.

---

## Scripts Reference

All commands go through `scripts.go` — no `make`, no shell, just Go:

| Command | What it does |
|---|---|
| `go run scripts.go run` | Start the API server |
| `go run scripts.go test` | Run integration tests with verbose output |
| `go run scripts.go build` | Compile binary to `bin/server` |
| `go run scripts.go docker-build` | Build Docker image `gateway-microcontroller` |
| `go run scripts.go swagger` | Regenerate `docs/` from source annotations |
