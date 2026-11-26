// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv_test

// Import packages
import (
	"context"           // Import the context package for request deadline assertions.
	"errors"            // Import the errors package for client and server error messages.
	"io"                // Import the io package for a simulated response write error.
	"net/http"          // Import the net/http package for HTTP methods and status codes.
	"net/http/httptest" // Import the net/http/httptest package for socket-free requests and recorders.
	"testing"           // Import the testing package for table-driven tests.
	"time"              // Import the time package for deadline comparisons.

	"github.com/thorsphere/lpsrv" // Import the lpsrv package under test.
	"github.com/thorsphere/tserr" // Import the tserr package for assertion errors.
)

// TestRequireMethodAllow verifies that a matching method leaves the response untouched and every rejection commits Allow.
func TestRequireMethodAllow(t *testing.T) {
	// Define cases for matching requests and empty or populated permitted sets.
	tests := []struct {
		name      string   // The subtest name.
		method    string   // The request method, or empty for a nil request.
		allowed   []string // The candidate methods passed to RequireMethod.
		wantAllow string   // The expected Allow header value.
		wantMatch bool     // Whether the request should pass without a response.
	}{
		{name: "no methods", method: http.MethodPost},
		{name: "only blank methods", method: http.MethodPost, allowed: []string{"", " \t"}},
		{name: "unique permitted methods", method: http.MethodPost, allowed: []string{" GET ", "", http.MethodHead, http.MethodGet}, wantAllow: "GET, HEAD"},
		{name: "case-sensitive mismatch", method: "get", allowed: []string{http.MethodGet}, wantAllow: "GET"},
		{name: "nil request", allowed: []string{http.MethodGet}, wantAllow: "GET"},
		{name: "exact match", method: http.MethodPost, allowed: []string{http.MethodPost}, wantMatch: true},
	}
	// Run each case with its request method and permitted list.
	for _, tt := range tests {
		// Check the guard result and the committed HTTP response.
		t.Run(tt.name, func(t *testing.T) {
			// Create a recorder without opening a socket.
			w := httptest.NewRecorder()
			// Clear the default status so any header write is observable.
			w.Code = 0
			// Leave the request nil unless the case specifies a method.
			var r *http.Request
			// Build a request for cases that specify a method.
			if tt.method != "" {
				// Use the case's method verbatim.
				r = httptest.NewRequest(tt.method, "/", nil)
			}
			// Check whether the guard matched the request as expected.
			if got := lpsrv.RequireMethod(w, r, "test", tt.allowed...); got != tt.wantMatch {
				// Report the unexpected result before inspecting the response.
				t.Fatalf("RequireMethod = %t, want %t", got, tt.wantMatch)
			}
			// Check that accepting the request did not touch the response.
			if tt.wantMatch {
				// Check the recorder for status, header, and body changes.
				if w.Code != 0 || len(w.Header()) != 0 || w.Body.Len() != 0 {
					// Report all observed response changes.
					t.Errorf("matching method wrote status %d, headers %v, body %q", w.Code, w.Header(), w.Body.String())
				}
				// Skip rejection assertions for a matching request.
				return
			}
			// Check the 405 status and read the committed response headers.
			resp := recordedHandlerResponse(t, w, http.StatusMethodNotAllowed)
			// Check header presence separately so an empty value cannot mask an absent header.
			if _, ok := resp.Header["Allow"]; !ok {
				// Report the missing header before comparing its value.
				t.Fatal("405 response is missing the Allow header")
			}
			// Check the advertised method list, including an explicitly empty value.
			checkHandlerString(t, "Allow", resp.Header.Get("Allow"), tt.wantAllow)
		})
	}
}

// TestRequestPath verifies that request diagnostics use only the URL path and tolerate missing URLs.
func TestRequestPath(t *testing.T) {
	// Define cases for ordinary and incomplete requests.
	tests := []struct {
		name string        // The subtest name.
		r    *http.Request // The request passed to the helper.
		want string        // The expected diagnostic path.
	}{
		{name: "path without query", r: httptest.NewRequest(http.MethodGet, "/health?probe=ready", nil), want: "/health"},
		{name: "nil URL", r: &http.Request{}},
		{name: "nil request"},
	}
	// Check the path returned for each request.
	for _, tt := range tests {
		// Compare the helper's return value within the named case.
		t.Run(tt.name, func(t *testing.T) {
			// Check that only the expected path is returned.
			if got := lpsrv.RequestPath(tt.r); got != tt.want {
				// Report the actual and expected paths.
				t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "request path", Actual: got, Want: tt.want}))
			}
		})
	}
}

// failingHealthWriter records health response headers but rejects body writes.
type failingHealthWriter struct {
	http.ResponseWriter     // The recorder interface, hiding its WriteString fast path.
	writes              int // The number of attempted body writes.
}

// Write rejects p and records the failed attempt.
func (w *failingHealthWriter) Write(p []byte) (int, error) {
	// Record the attempt without writing the payload.
	w.writes++
	// Simulate a closed connection.
	return 0, io.ErrClosedPipe
}

// TestHealthHandler verifies probe responses and behavior after a failed body write.
func TestHealthHandler(t *testing.T) {
	// Define cases for supported methods, a rejected method, and a write failure.
	tests := []struct {
		name       string // The subtest name.
		method     string // The HTTP method sent to the health handler.
		failWrite  bool   // Whether a body write should fail.
		wantStatus int    // The expected HTTP response status.
		wantBody   string // The expected response body for supported methods.
	}{
		{name: "GET liveness probe", method: http.MethodGet, wantStatus: http.StatusOK, wantBody: "ok\n"},
		{name: "HEAD readiness probe", method: http.MethodHead, wantStatus: http.StatusOK},
		{name: "POST rejected", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
		{name: "GET body write fails", method: http.MethodGet, failWrite: true, wantStatus: http.StatusOK},
	}
	// Exercise the handler independently for each probe.
	for _, tt := range tests {
		// Check the response and any failed write attempts.
		t.Run(tt.name, func(t *testing.T) {
			// Record the response without a listener.
			w := httptest.NewRecorder()
			// Set up a failing writer backed by the same recorder.
			failing := &failingHealthWriter{ResponseWriter: w}
			// Use the regular recorder unless the case simulates a failed write.
			var writer http.ResponseWriter = w
			// Select the failing writer for the failure case.
			if tt.failWrite {
				// Record the headers but reject the body write.
				writer = failing
			}
			// Send the probe to the health handler.
			lpsrv.HealthHandler().ServeHTTP(writer, httptest.NewRequest(tt.method, "/health", nil))
			// Check the committed status and headers.
			resp := recordedHandlerResponse(t, w, tt.wantStatus)
			// Compare the content type for all probe responses.
			checkHandlerString(t, "Content-Type", resp.Header.Get("Content-Type"), "text/plain; charset=utf-8")
			// Check the permitted methods on the rejection response.
			if tt.wantStatus == http.StatusMethodNotAllowed {
				// Verify that both supported methods are advertised.
				checkHandlerString(t, "Allow", resp.Header.Get("Allow"), "GET, HEAD")
			} else {
				// Check the response body, including that HEAD omits it.
				checkHandlerString(t, "response body", w.Body.String(), tt.wantBody)
			}
			// Confirm the handler attempted one write and did not retry on failure.
			if tt.failWrite && failing.writes != 1 {
				// Report unexpected write attempts.
				t.Errorf("body write attempts = %d, want 1", failing.writes)
			}
		})
	}
}

// TestWriteError verifies which error details and caller-supplied headers reach the client.
func TestWriteError(t *testing.T) {
	// Reuse a distinctive error to distinguish client-facing messages from sanitized ones.
	detail := errors.New("specific error details")
	// Define cases for client errors, server errors, and invalid statuses.
	tests := []struct {
		name        string // The subtest name.
		status      int    // The requested response status.
		err         error  // The error supplied to WriteError.
		header      string // The optional protocol header supplied by the caller.
		headerValue string // The value to preserve for that header.
		wantStatus  int    // The committed response status.
		wantBody    string // The expected response body.
	}{
		{name: "client error detail", status: http.StatusBadRequest, err: detail, wantStatus: http.StatusBadRequest, wantBody: "specific error details\n"},
		{name: "client status text", status: http.StatusNotFound, wantStatus: http.StatusNotFound, wantBody: "Not Found\n"},
		{name: "preserved challenge", status: http.StatusUnauthorized, header: "WWW-Authenticate", headerValue: "Bearer", wantStatus: http.StatusUnauthorized, wantBody: "Unauthorized\n"},
		{name: "preserved method list", status: http.StatusMethodNotAllowed, header: "Allow", headerValue: "GET, HEAD", wantStatus: http.StatusMethodNotAllowed, wantBody: "Method Not Allowed\n"},
		{name: "sanitized server error", status: http.StatusBadGateway, err: detail, wantStatus: http.StatusBadGateway, wantBody: "Bad Gateway\n"},
		{name: "server error without detail", status: http.StatusServiceUnavailable, wantStatus: http.StatusServiceUnavailable, wantBody: "Service Unavailable\n"},
		{name: "below error range", status: http.StatusOK, err: detail, wantStatus: http.StatusInternalServerError, wantBody: "Internal Server Error\n"},
		{name: "above error range", status: 600, err: detail, wantStatus: http.StatusInternalServerError, wantBody: "Internal Server Error\n"},
		{name: "unrecognized error status", status: 499, wantStatus: http.StatusInternalServerError, wantBody: "Internal Server Error\n"},
	}
	// Check the response for each requested status.
	for _, tt := range tests {
		// Compare one recorded response with the public contract.
		t.Run(tt.name, func(t *testing.T) {
			// Record the response without a listener.
			w := httptest.NewRecorder()
			// Set the caller-supplied header when applicable.
			if tt.header != "" {
				// Supply the required protocol information before writing the error.
				w.Header().Set(tt.header, tt.headerValue)
			}
			// Write the case's error response.
			lpsrv.WriteError(w, tt.status, tt.err)
			// Check the committed HTTP status.
			resp := recordedHandlerResponse(t, w, tt.wantStatus)
			// Compare the full response body to catch unintended error details.
			checkHandlerString(t, "response body", w.Body.String(), tt.wantBody)
			// Verify the caller-supplied header survived the error write.
			if tt.header != "" {
				// Compare the committed header value.
				checkHandlerString(t, tt.header, resp.Header.Get(tt.header), tt.headerValue)
			}
		})
	}
}

// TestTimeoutMiddleware verifies deadline selection, child cancellation, and the missing-handler response.
func TestTimeoutMiddleware(t *testing.T) {
	// Define cases for explicit and default budgets, an earlier parent deadline, and a missing handler.
	tests := []struct {
		name          string        // The subtest name.
		timeout       time.Duration // The timeout passed to the middleware.
		wantTimeout   time.Duration // The handler's expected timeout budget.
		parentTimeout time.Duration // The incoming deadline budget, or zero for none.
		nilHandler    bool          // Whether to pass a nil downstream handler.
	}{
		{name: "explicit timeout", timeout: time.Minute, wantTimeout: time.Minute},
		{name: "zero uses default", wantTimeout: 30 * time.Second},
		{name: "negative uses default", timeout: -time.Second, wantTimeout: 30 * time.Second},
		{name: "earlier parent deadline", timeout: 2 * time.Minute, parentTimeout: time.Minute},
		{name: "nil handler", nilHandler: true},
	}
	// Dispatch requests directly through the middleware.
	for _, tt := range tests {
		// Check the downstream context or the sanitized failure response.
		t.Run(tt.name, func(t *testing.T) {
			// Prepare a socket-free request and response recorder.
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			// Record the status and body written by the middleware or handler.
			w := httptest.NewRecorder()
			// Attach an incoming deadline when the case supplies one.
			if tt.parentTimeout != 0 {
				// Derive the parent deadline from the original context.
				parent, cancel := context.WithTimeout(r.Context(), tt.parentTimeout)
				// Release the parent timer after the assertions.
				defer cancel()
				// Forward the request carrying the parent deadline.
				r = r.WithContext(parent)
			}
			// Retain the downstream context for inspection after the handler returns.
			var child context.Context
			// Leave the handler nil unless the case expects a downstream call.
			var next http.Handler
			// Install a downstream handler for deadline tests.
			if !tt.nilHandler {
				// Capture the downstream context and write a recognizable status.
				next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Save the context so cancellation can be checked after dispatch.
					child = r.Context()
					// Confirm the context is still usable during the handler call.
					if err := child.Err(); err != nil {
						// Report premature cancellation.
						t.Error(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "downstream context", Err: err}))
					}
					// Write a status the caller can distinguish from the default.
					w.WriteHeader(http.StatusNoContent)
				})
			}
			// Construct the middleware before measuring the deadline bounds.
			handler := lpsrv.TimeoutMiddleware(tt.timeout, next)
			// Take the lower time bound immediately before dispatch.
			before := time.Now()
			// Process the request without a listener or a timer wait.
			handler.ServeHTTP(w, r)
			// Take the upper time bound immediately after dispatch.
			after := time.Now()
			// Check that the missing handler produces a sanitized 500.
			if tt.nilHandler {
				// Verify the committed failure status.
				recordedHandlerResponse(t, w, http.StatusInternalServerError)
				// Verify that no configuration details appear in the body.
				checkHandlerString(t, "response body", w.Body.String(), "Internal Server Error\n")
				// Skip downstream context assertions when no handler was configured.
				return
			}
			// Verify that the downstream response reaches the caller.
			recordedHandlerResponse(t, w, http.StatusNoContent)
			// Require a context before inspecting the deadline or cancellation.
			if child == nil {
				// Report the missing downstream invocation.
				t.Fatal(tserr.NilFailed("downstream context"))
			}
			// Compute the bounds for a deadline derived during dispatch.
			earliest, latest := before.Add(tt.wantTimeout), after.Add(tt.wantTimeout)
			// Use the exact parent deadline when it is earlier than the middleware budget.
			if parentDeadline, ok := r.Context().Deadline(); ok {
				// Require the incoming deadline to be preserved.
				earliest, latest = parentDeadline, parentDeadline
			}
			// Verify the deadline without waiting for it to expire.
			if deadline, ok := child.Deadline(); !ok || deadline.Before(earliest) || deadline.After(latest) {
				// Report the observed deadline and expected bounds.
				t.Errorf("downstream deadline = %v (present: %t), want between %v and %v", deadline, ok, earliest, latest)
			}
			// Verify the child context is canceled as soon as the handler returns.
			if err := child.Err(); err != context.Canceled {
				// Report missing or unexpected cancellation.
				t.Errorf("downstream context error = %v, want %v", err, context.Canceled)
			}
			// Verify canceling the child did not cancel the incoming request.
			if err := r.Context().Err(); err != nil {
				// Report unexpected cancellation of the parent.
				t.Error(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "incoming context", Err: err}))
			}
		})
	}
}

// recordedHandlerResponse returns the committed response and checks its status.
func recordedHandlerResponse(t *testing.T, w *httptest.ResponseRecorder, wantStatus int) *http.Response {
	// Attribute assertion failures to the calling test.
	t.Helper()
	// Read the committed status and header snapshot.
	resp := w.Result()
	// Close the recorded body when the subtest ends.
	t.Cleanup(func() {
		// Check for an unexpected response close error.
		if err := resp.Body.Close(); err != nil {
			// Report the unexpected close error.
			t.Error(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "close response body", Err: err}))
		}
	})
	// Check that the handler committed the expected status.
	if resp.StatusCode != wantStatus {
		// Report the actual and expected statuses.
		t.Errorf("status = %d, want %d", resp.StatusCode, wantStatus)
	}
	// Return the response so the caller can inspect its headers.
	return resp
}

// checkHandlerString reports a mismatch between a recorded string and its expected value.
func checkHandlerString(t *testing.T, label, got, want string) {
	// Attribute assertion failures to the calling test.
	t.Helper()
	// Compare the values before constructing the assertion error.
	if got != want {
		// Report the labeled string mismatch.
		t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: label, Actual: got, Want: want}))
	}
}
