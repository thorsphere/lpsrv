// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

// Package lpsrv provides composable HTTP server primitives, hardened
// middleware, strict size-bounded JSON codecs, health probes, and Google Cloud
// Run lifecycle management built on standard net/http.
//
// # Key capabilities
//
//   - Bearer authentication: [ValidateBearerToken] checks RFC 6750 token
//     syntax. [RequireBearerAuth] compares fixed-length SHA-256 digests in
//     constant time and sends a Bearer challenge on unauthorized requests.
//   - Probes, method guards, and deadlines: [HealthHandler] supports GET and
//     HEAD, and [RequireMethod] sets Allow on 405 responses. [TimeoutMiddleware]
//     sets a per-request context deadline; handlers must honor cancellation.
//   - Strict JSON codecs and error responses: [DecodeJSON] bounds request body
//     sizes and rejects unknown fields and trailing data. [WriteJSON] buffers
//     JSON before writing headers and returns a sanitized 500 on marshaling
//     failure. [WriteError] uses standard HTTP status text for 5xx responses.
//   - Server configuration and Cloud Run lifecycle: [NewHTTPServer] configures
//     a server at an explicit address. [Option] values customize settings,
//     including the buffer that keeps the socket write timeout above the
//     configured request timeout. [Run] and [RunContext] listen on the port
//     specified by PORT, defaulting to 8080 when unset or blank. [Run] handles
//     SIGINT and SIGTERM; [RunContext] drains active requests on context
//     cancellation and force-closes active connections if the drain deadline
//     expires. [RequireEnv] reads a required, nonblank environment variable.
package lpsrv
