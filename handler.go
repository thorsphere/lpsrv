// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"context"  // Import the context package for per-request timeout deadlines.
	"io"       // Import the io package for writing plain-text probe responses.
	"net/http" // Import the net/http package for HTTP handlers, headers, and status codes.
	"slices"   // Import the slices package for deduplicating allowed HTTP methods.
	"strings"  // Import the strings package for trimming and joining HTTP method names.
	"time"     // Import the time package for timeout durations.

	"github.com/thorsphere/tserr" // Import the tserr package for structured error construction.
	"github.com/thorsphere/tslog" // Import the tslog package for structured logging.
)

// The following constants define HTTP headers, media types, and payloads used by standard handlers.
const (
	allowHeader          = "Allow"                     // The RFC 9110 response header listing permitted HTTP methods on 405 responses.
	contentTypeHeader    = "Content-Type"              // The HTTP response header declaring the payload media type.
	plainTextContentType = "text/plain; charset=utf-8" // The media type written on plain-text health probe responses.
	healthResponseBody   = "ok\n"                      // The plain-text body written on 200 OK GET health probes.
	healthResourceName   = "health"                    // The resource label reported in 405 errors on health endpoints.
)

// requestPath returns the URL path from r, or an empty string if r or its URL
// is nil.
func requestPath(r *http.Request) string {
	// Guard against nil requests or synthetic requests with a nil URL.
	if r == nil || r.URL == nil {
		// Return an empty path when no request URL is available.
		return ""
	}
	// Return the request URL path.
	return r.URL.Path
}

// RequireMethod reports whether r uses an HTTP method listed in allowed for a
// specific endpoint, and otherwise sets the RFC 9110 Allow header on w and
// writes a 405 Method Not Allowed response for resource. Callers should pass
// only the method or methods valid for that endpoint: pass a single method
// (such as http.MethodPost) to require an exact match, or multiple methods
// (such as http.MethodGet and http.MethodHead) only when that same endpoint
// handles all of them.
func RequireMethod(w http.ResponseWriter, r *http.Request, resource string, allowed ...string) bool {
	// Default the incoming request method to empty when r is nil.
	method := ""
	// Read the incoming HTTP method when r is non-nil.
	if r != nil {
		// Capture the request method string.
		method = r.Method
	}
	// Collect unique non-empty allowed method names for matching and the Allow header.
	permitted := make([]string, 0, len(allowed))
	// Track whether the incoming method matches any permitted method.
	matched := false
	// Inspect each candidate HTTP method supplied by the caller.
	for _, m := range allowed {
		// Trim surrounding whitespace from the candidate method name.
		trimmed := strings.TrimSpace(m)
		// Skip blank method entries so the Allow header stays well-formed.
		if trimmed == "" {
			// Continue to the next candidate method.
			continue
		}
		// Append the candidate method if it has not already been recorded.
		if !slices.Contains(permitted, trimmed) {
			// Record the unique allowed method for the Allow header.
			permitted = append(permitted, trimmed)
		}
		// Check whether the incoming request method matches this allowed method.
		if method == trimmed {
			// Mark the incoming method as permitted.
			matched = true
		}
	}
	// Return true without modifying w when the request method is permitted.
	if matched {
		// Report that method validation succeeded.
		return true
	}
	// Set the RFC 9110 Allow header even when no methods are permitted.
	w.Header().Set(allowHeader, strings.Join(permitted, ", "))
	// Write the structured 405 Method Not Allowed error response.
	http.Error(w, tserr.MethodNotAllowed(&tserr.MethodNotAllowedArgs{Method: method, Resource: resource}).Error(), http.StatusMethodNotAllowed)
	// Report that the incoming request method was rejected.
	return false
}

// HealthHandler returns an http.HandlerFunc that responds to GET and HEAD
// liveness or readiness probes with 200 OK, omitting the response body on HEAD
// requests and rejecting all other HTTP methods with 405 Method Not Allowed.
func HealthHandler() http.HandlerFunc {
	// Return the stateless health probe handler function.
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow only GET and HEAD requests on the health probe endpoint.
		if !RequireMethod(w, r, healthResourceName, http.MethodGet, http.MethodHead) {
			// Stop processing the disallowed request.
			return
		}
		// Set the plain-text content type header for the probe response.
		w.Header().Set(contentTypeHeader, plainTextContentType)
		// Write the 200 OK status header.
		w.WriteHeader(http.StatusOK)
		// Omit the response body on HEAD requests per RFC 9110 semantics.
		if r.Method == http.MethodHead {
			// Return without writing a response body for HEAD probes.
			return
		}
		// Write the plain-text health indicator body and log any write failure.
		if _, err := io.WriteString(w, healthResponseBody); err != nil {
			// Log the response write failure for server diagnostics.
			tslog.Error(tserr.Op(&tserr.OpArgs{Op: "write health response", Fn: requestPath(r), Err: err}))
		}
	}
}

// writeInternalError logs err via tslog when err is non-nil and writes a
// sanitized 500 Internal Server Error response to w without exposing internal
// error details to the client.
func writeInternalError(w http.ResponseWriter, err error) {
	// Log the underlying server error when one was provided.
	if err != nil {
		// Record the full error in structured server logs.
		tslog.Error(err)
	}
	// Write a generic 500 Internal Server Error message so upstream internals do not leak.
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// WriteError writes an HTTP error response with status to w: for 4xx status
// codes it writes err's message (or the standard HTTP status text when err is
// nil), for valid 5xx status codes it logs err and writes only the sanitized
// HTTP status text, and for out-of-range status codes it logs err and writes a
// sanitized 500 Internal Server Error. WriteError does not set protocol
// challenge or method headers; use RequireBearerAuth for 401 Unauthorized
// responses and RequireMethod for 405 Method Not Allowed responses (or set
// WWW-Authenticate / Allow on w before calling WriteError).
func WriteError(w http.ResponseWriter, status int, err error) {
	// Normalizestatus codes outside 400-599 or unrecognized status codes to 500.
	if status < http.StatusBadRequest || status > 599 || http.StatusText(status) == "" {
		// Log err and write a sanitized 500 Internal Server Error response.
		writeInternalError(w, err)
		// Stop after writing the fallback server error.
		return
	}
	// Sanitize 5xx responses so upstream errors are logged but never leaked to clients.
	if status >= http.StatusInternalServerError {
		// Log the underlying server error when one was provided.
		if err != nil {
			// Record the full error in structured server logs.
			tslog.Error(err)
		}
		// Write the standard 5xx status text without internal error details.
		http.Error(w, http.StatusText(status), status)
		// Stop after writing the sanitized 5xx response.
		return
	}
	// Default the 4xx client error message to the standard HTTP status text.
	msg := http.StatusText(status)
	// Prefer the structured validation error text when err is non-nil.
	if err != nil {
		// Use the caller's validation error message for the 4xx response body.
		msg = err.Error()
	}
	// Write the 4xx client error response.
	http.Error(w, msg, status)
}

// TimeoutMiddleware returns an http.Handler that bounds each request's context
// to timeout (falling back to defaultRequestTimeout when timeout is zero or
// negative) before invoking next, or responds with 500 Internal Server Error
// if next is nil.
func TimeoutMiddleware(timeout time.Duration, next http.Handler) http.Handler {
	// Fall back to defaultRequestTimeout when a non-positive timeout is supplied.
	if timeout <= 0 {
		// Apply the package default per-request deadline.
		timeout = defaultRequestTimeout
	}
	// Return an http.HandlerFunc that attaches a bounded context to each request.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Guard against a nil downstream handler so requests never panic.
		if next == nil {
			// Log and write a sanitized 500 Internal Server Error for the missing handler.
			writeInternalError(w, tserr.Empty("handler"))
			// Stop processing when no downstream handler is configured.
			return
		}
		// Derive a deadline-bounded context from the incoming request context.
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		// Release the timer resources when next returns.
		defer cancel()
		// Forward the request carrying the bounded context to next.
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
