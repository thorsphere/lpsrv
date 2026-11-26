# lpsrv

[![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/thorsphere/lpsrv)](https://pkg.go.dev/mod/github.com/thorsphere/lpsrv)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/thorsphere/lpsrv)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/thorsphere/lpsrv)
![GitHub Top Language](https://img.shields.io/github/languages/top/thorsphere/lpsrv)
[![CodeFactor](https://www.codefactor.io/repository/github/thorsphere/lpsrv/badge)](https://www.codefactor.io/repository/github/thorsphere/lpsrv)
![OSS Lifecycle](https://img.shields.io/osslifecycle/thorsphere/lpsrv)

---

Go HTTP server toolkit for Cloud Run and other services that need configurable request deadlines. Use it with standard `net/http` handlers and your own router; it is a library, not a server framework or standalone binary.

---

## Features

- **Server lifecycle:** Bind to Cloud Run's `PORT`, handle shutdown signals, and drain active requests within a configurable deadline.
- **Request deadlines and socket timeouts:** Keep the socket write timeout above the configured handler deadline so cooperative handlers have time to send an error response.
- **Bearer authentication:** Validate RFC 6750 token syntax and compare fixed-length SHA-256 digests in constant time. Rejected requests receive `401` with `WWW-Authenticate: Bearer`.
- **Health probes and method guards:** Serve `GET` and `HEAD` probes and advertise allowed methods on `405` responses.
- **JSON and error responses:** Bound request bodies, reject unknown struct fields and trailing data, and buffer JSON before committing the HTTP status. `WriteError` logs server errors and sends only standard status text for `5xx` responses.

---

## Getting started

Use Go 1.27 or later (see [`go.mod`](go.mod)). From an existing Go module, add the dependency:

```sh
go get github.com/thorsphere/lpsrv
```

Save the following as `main.go`. It exposes an unauthenticated health probe and protects a JSON endpoint with a bearer token. The example reads `API_TOKEN` explicitly; `lpsrv` does not read it automatically.

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/thorsphere/lpsrv"
)

func main() {
	token, err := lpsrv.RequireEnv("API_TOKEN")
	if err != nil {
		log.Fatal(err)
	}
	if err := lpsrv.ValidateBearerToken(token); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", lpsrv.HealthHandler())
	mux.Handle("/echo", lpsrv.RequireBearerAuth(token, http.HandlerFunc(echo)))

	if err := lpsrv.Run(mux, lpsrv.WithRequestTimeout(2*time.Minute)); err != nil {
		log.Fatal(err)
	}
}

func echo(w http.ResponseWriter, r *http.Request) {
	if !lpsrv.RequireMethod(w, r, "echo", http.MethodPost) {
		return
	}

	type message struct {
		Text string `json:"text"`
	}
	const maxBodyBytes = 4 << 10 // 4 KiB
	payload, status, err := lpsrv.DecodeJSON[message](w, r, maxBodyBytes)
	if err != nil {
		lpsrv.WriteError(w, status, err)
		return
	}
	lpsrv.WriteJSON(w, r, http.StatusOK, payload)
}
```

Set `API_TOKEN` in your shell to a token containing no whitespace and run `go run .`. With `PORT` unset, the server listens on port `8080`. From another shell (with the same token set), try:

```sh
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/echo \
  -H "Authorization: Bearer ${API_TOKEN}" \
  -H 'Content-Type: application/json' \
  --data '{"text":"hello"}'
```

The probe returns `ok`, and the authenticated endpoint returns `{"text":"hello"}`. The local requests use plain HTTP; use HTTPS for bearer-authenticated traffic outside local development.

---

## API reference

| API | Purpose |
| --- | --- |
| [`Run`](https://pkg.go.dev/github.com/thorsphere/lpsrv#Run) | Serve on `PORT` and shut down on `SIGINT` or `SIGTERM`. |
| [`RunContext`](https://pkg.go.dev/github.com/thorsphere/lpsrv#RunContext) | Serve on `PORT` and shut down when the caller's context is canceled. |
| [`NewHTTPServer`](https://pkg.go.dev/github.com/thorsphere/lpsrv#NewHTTPServer) | Configure an `http.Server` at an explicit address without starting it. The caller owns startup and shutdown. |
| [`Option`](https://pkg.go.dev/github.com/thorsphere/lpsrv#Option) | Configure server settings; see the defaults below. |
| [`RequireEnv`](https://pkg.go.dev/github.com/thorsphere/lpsrv#RequireEnv) | Read a required environment variable. Reject unset or whitespace-only values without trimming the returned value. |
| [`ValidateBearerToken`](https://pkg.go.dev/github.com/thorsphere/lpsrv#ValidateBearerToken), [`RequireBearerAuth`](https://pkg.go.dev/github.com/thorsphere/lpsrv#RequireBearerAuth) | Validate token syntax at startup and protect an `http.Handler`. |
| [`HealthHandler`](https://pkg.go.dev/github.com/thorsphere/lpsrv#HealthHandler), [`RequireMethod`](https://pkg.go.dev/github.com/thorsphere/lpsrv#RequireMethod) | Serve a `GET`/`HEAD` probe and reject unsupported methods with `405` and `Allow`. |
| [`TimeoutMiddleware`](https://pkg.go.dev/github.com/thorsphere/lpsrv#TimeoutMiddleware) | Set a request context deadline when composing middleware independently of the server constructors. |
| [`DecodeJSON`](https://pkg.go.dev/github.com/thorsphere/lpsrv#DecodeJSON) | Decode one JSON value with a positive byte limit. Return a status and error for the caller to handle, including `400` for invalid bodies and `413` when the read exceeds the limit. |
| [`WriteJSON`](https://pkg.go.dev/github.com/thorsphere/lpsrv#WriteJSON), [`WriteError`](https://pkg.go.dev/github.com/thorsphere/lpsrv#WriteError) | Write buffered JSON or a plain-text HTTP error. `WriteJSON` omits bodies for `HEAD` and for statuses `204`, `205`, and `304`. |

## Configuration and Cloud Run deployment

| Option | Default | Controls |
| --- | --- | --- |
| `WithName` | `Server` | Server name in startup logs. |
| `WithReadHeaderTimeout` | `5s` | Socket deadline for reading request headers. |
| `WithReadTimeout` | `15s` | Socket deadline for reading the entire request, including the body. |
| `WithRequestTimeout` | `30s` | Request context deadline; an earlier parent deadline still applies. |
| `WithWriteTimeoutBuffer` | `15s` | Added to the configured request timeout to derive `http.Server.WriteTimeout` (`45s` by default). |
| `WithIdleTimeout` | `60s` | Maximum idle time between requests on keep-alive connections. |
| `WithShutdownTimeout` | `8s` | Time for `Run` and `RunContext` to drain active requests before force-closing active connections. |

Duration options must be positive. `Run` and `RunContext` listen on all interfaces using `PORT`, falling back to `8080` with a warning if it is unset, blank, or a bare `:`. On Cloud Run, let the platform supply `PORT`, inject `API_TOKEN` from a secret if you use the example's authentication, and configure an HTTP probe at `/healthz` if needed.

For long-running requests, configure both the Cloud Run request timeout and the library's request deadline. Leave enough time between those deadlines to send an error response before the platform times out. Keep the shutdown drain budget within Cloud Run's 10-second termination window; increasing the request deadline does not extend that window.

---

## Known limitations

- Request deadlines are cooperative. Pass `r.Context()` to downstream work and handle cancellation; the middleware does not interrupt a handler or write a timeout response for it.
- The health handler is a static probe. Add your own readiness handler if readiness depends on a database, upstream service, or other dependency.
- Bearer authentication checks a single shared token. It does not provide JWT or OAuth validation or token rotation. The lifecycle helpers serve plain HTTP; terminate TLS at Cloud Run or another trusted ingress in production.
- `DecodeJSON` does not enforce the request `Content-Type` or validate application-level fields. `WriteJSON` buffers the whole payload in memory and is not a streaming encoder.

---

## Documentation & Resources

- [Go Package Documentation](https://pkg.go.dev/github.com/thorsphere/lpsrv): Complete API reference
- [Open Source Insights](https://deps.dev/go/github.com%2Fthorsphere%2Flpsrv): Dependency analysis
- [Cloud Run container runtime contract](https://cloud.google.com/run/docs/container-contract): Port binding and shutdown behavior

## Contributing and security

- Read the [contribution policy](CONTRIBUTING.md) and [code of conduct](CODE_OF_CONDUCT.md) before contributing.
- Follow the [security policy](SECURITY.md) to report vulnerabilities privately.

---

## ⚖️ License & Commercial Usage

Copyright (c) 2026 thorsphere. All rights reserved.

This project is licensed under the **Functional Source License v1.1 (FSL-1.1-ALv2)**. 

* The use, modification, and redistribution of this Go package is completely free for private, educational, non-commercial, and internal purposes. 
* If you are a company or institution looking to use this package in a commercial product, service, or business environment, you must secure a commercial license.
* Each version of this software automatically converts to the fully open-source Apache License, Version 2.0 on the second anniversary of its release.

For full details, please see the [LICENSE](LICENSE.md) file.

### 💼 Commercial Licensing & Inquiries

To purchase a commercial license or discuss support options, please reach out directly:

* 📩 **Contact:** business at thorsphere dot com
* 💬 **Response Time:** Usually within a couple of business days.

*Please include your company name and a brief overview of your use case so I can provide the right licensing details.*





