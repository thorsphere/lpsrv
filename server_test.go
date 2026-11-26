// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv_test

// Import packages
import (
	"context"           // Import the context package for incoming request deadlines.
	"errors"            // Import the errors package for matching lifecycle errors.
	"fmt"               // Import the fmt package for wrapping simulated lifecycle errors.
	"net/http"          // Import the net/http package for handlers and status codes.
	"net/http/httptest" // Import the net/http/httptest package for socket-free requests.
	"os"                // Import the os package for testing an unset environment variable.
	"slices"            // Import the slices package for comparing signal subscriptions.
	"sync/atomic"       // Import the sync/atomic package for tracking unexpected listener calls.
	"syscall"           // Import the syscall package for the Cloud Run termination signal.
	"testing"           // Import the testing package for table-driven tests.
	"time"              // Import the time package for deadline and timeout assertions.

	"github.com/thorsphere/lpsrv" // Import the lpsrv package under test.
	"github.com/thorsphere/tserr" // Import the tserr package for assertion errors.
)

// TestRequireEnv verifies key validation and that nonblank values are returned unchanged.
func TestRequireEnv(t *testing.T) {
	// Use a dedicated key so the test does not depend on server configuration.
	const envKey = "LPSRV_TEST_REQUIRE_ENV"
	// Cover blank keys, missing or blank values, and whitespace preservation.
	tests := []struct {
		name    string // The subtest name.
		key     string // The key passed to RequireEnv.
		value   string // The value installed under envKey.
		unset   bool   // Whether to remove envKey after registering cleanup.
		want    string // The expected returned value.
		wantErr error  // The expected validation error, or nil on success.
	}{
		{name: "empty key", wantErr: tserr.Empty("environment variable name")},
		{name: "whitespace-only key", key: " \t\n", wantErr: tserr.Empty("environment variable name")},
		{name: "unset variable", key: envKey, unset: true, wantErr: tserr.NotSet(envKey)},
		{name: "empty value", key: envKey, wantErr: tserr.NotSet(envKey)},
		{name: "whitespace-only value", key: envKey, value: " \t\n", wantErr: tserr.NotSet(envKey)},
		{name: "nonblank value", key: envKey, value: "configured", want: "configured"},
		{name: "trim key and preserve raw value", key: " \t" + envKey + "\n", value: " \tconfigured\n", want: " \tconfigured\n"},
	}
	// Run serially because the subtests modify the process environment.
	for _, tt := range tests {
		// Set up and check one environment state.
		t.Run(tt.name, func(t *testing.T) {
			// Install the test value and register restoration of the original environment.
			t.Setenv(envKey, tt.value)
			// Remove the variable when the case requires it to be absent rather than empty.
			if tt.unset {
				// Fail if the environment could not be configured as requested.
				if err := os.Unsetenv(envKey); err != nil {
					// Report the setup failure before testing RequireEnv.
					t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "os.Unsetenv", Err: err}))
				}
			}
			// Look up the requested key.
			got, err := lpsrv.RequireEnv(tt.key)
			// Report any error in a successful lookup case.
			if tt.wantErr == nil && err != nil {
				// Stop before checking a value from a failed lookup.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "RequireEnv", Err: err}))
			}
			// Verify the validation error when the lookup is expected to fail.
			if tt.wantErr != nil {
				// Require an error before comparing its diagnostic.
				if err == nil {
					// Report the missing validation error.
					t.Fatal(tserr.NilFailed("RequireEnv"))
				}
				// Distinguish invalid keys from missing values in the returned diagnostic.
				if err.Error() != tt.wantErr.Error() {
					// Report the unexpected diagnostic.
					t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "RequireEnv error", Actual: err.Error(), Want: tt.wantErr.Error()}))
				}
			}
			// Require the exact value, including surrounding whitespace when present.
			if got != tt.want {
				// Report the actual and expected values.
				t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "RequireEnv value", Actual: got, Want: tt.want}))
			}
		})
	}
}

// TestRunContextListenAddress verifies how PORT determines the wildcard address.
func TestRunContextListenAddress(t *testing.T) {
	// Cover defaulting, whitespace trimming, and an optional colon prefix.
	tests := []struct {
		name  string // The subtest name.
		port  string // The value installed for PORT.
		unset bool   // Whether PORT should be absent rather than empty.
		want  string // The expected wildcard listen address.
	}{
		{name: "unset", unset: true, want: ":8080"},
		{name: "empty", want: ":8080"},
		{name: "whitespace only", port: " \t\n", want: ":8080"},
		{name: "bare colon", port: ":", want: ":8080"},
		{name: "padded port", port: " \t9090\n", want: ":9090"},
		{name: "colon-prefixed port", port: ":9090", want: ":9090"},
	}
	// Run serially because the cases change the environment and listener seam.
	for _, tt := range tests {
		// Exercise one PORT configuration.
		t.Run(tt.name, func(t *testing.T) {
			// Install PORT and register restoration of its original value.
			t.Setenv("PORT", tt.port)
			// Remove PORT for the unset case.
			if tt.unset {
				// Fail if PORT could not be removed.
				if err := os.Unsetenv("PORT"); err != nil {
					// Report the setup failure.
					t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "os.Unsetenv", Err: err}))
				}
			}
			// Observe the address without binding it.
			var addr string
			// Capture the configured server in the fake listener.
			t.Cleanup(lpsrv.SetListenAndServe(func(srv *http.Server) error {
				// Record the address before returning the listener result.
				addr = srv.Addr
				// Exit normally without opening a socket.
				return http.ErrServerClosed
			}))
			// Wait for RunContext to collect the listener result.
			if err := lpsrv.RunContext(context.Background(), http.NotFoundHandler()); err != nil {
				// Report the unexpected lifecycle failure.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "RunContext", Err: err}))
			}
			// Check the exact address that would have been bound.
			if addr != tt.want {
				// Report an unexpected listen address.
				t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "server address", Actual: addr, Want: tt.want}))
			}
		})
	}
}

// TestNewHTTPServerInputs verifies address normalization and required inputs.
func TestNewHTTPServerInputs(t *testing.T) {
	// Use a valid handler in cases that exercise address handling.
	handler := http.NotFoundHandler()
	// Cover required inputs and the normalized address on success.
	tests := []struct {
		name     string       // The subtest name.
		addr     string       // The supplied listen address.
		handler  http.Handler // The supplied HTTP handler.
		wantAddr string       // The expected normalized address.
		wantErr  error        // The expected validation error, if any.
	}{
		{name: "empty address", handler: handler, wantErr: tserr.Empty("address")},
		{name: "blank address", addr: " \t\n", handler: handler, wantErr: tserr.Empty("address")},
		{name: "nil handler", addr: ":8080", wantErr: tserr.Empty("handler")},
		{name: "padded address", addr: " \t:8080\n", handler: handler, wantAddr: ":8080"},
	}
	// Exercise each case without starting a listener.
	for _, tt := range tests {
		// Construct a server with the scripted inputs.
		t.Run(tt.name, func(t *testing.T) {
			// Resolve the address and handler.
			srv, err := lpsrv.NewHTTPServer(tt.addr, tt.handler)
			// Check that validation failures do not return a server.
			if tt.wantErr != nil {
				// Require a nil server in the error case.
				if srv != nil {
					// Report unexpected construction.
					t.Error("NewHTTPServer returned a server for invalid inputs")
				}
				// Check the validation error.
				checkServerInputError(t, "NewHTTPServer", err, tt.wantErr)
				// Skip the address check for invalid inputs.
				return
			}
			// Fail if valid inputs were rejected.
			if err != nil {
				// Report the unexpected constructor error.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "NewHTTPServer", Err: err}))
			}
			// Require a server before reading its address.
			if srv == nil {
				// Report a missing server.
				t.Fatal(tserr.Empty("HTTP server"))
			}
			// Check that whitespace was removed from the address.
			if srv.Addr != tt.wantAddr {
				// Report the actual and expected address.
				t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "server address", Actual: srv.Addr, Want: tt.wantAddr}))
			}
		})
	}
}

// TestRunContextInvalidInputs verifies that invalid settings are rejected before listening.
func TestRunContextInvalidInputs(t *testing.T) {
	// Use valid inputs except for the one being tested.
	ctx, handler := context.Background(), http.NotFoundHandler()
	// Use a distinct error to identify unexpected listener startup.
	listenerErr := errors.New("listener called during input validation")
	// Cover the input checks and option resolution failures.
	tests := []struct {
		name    string          // The subtest name.
		ctx     context.Context // The supplied lifecycle context.
		handler http.Handler    // The supplied HTTP handler.
		opts    []lpsrv.Option  // The supplied server options.
		wantErr error           // The expected validation error.
	}{
		{name: "nil context", handler: handler, wantErr: tserr.Empty("context")},
		{name: "nil handler", ctx: ctx, wantErr: tserr.Empty("handler")},
		{name: "invalid option", ctx: ctx, handler: handler, opts: []lpsrv.Option{lpsrv.WithName(" ")}, wantErr: tserr.Empty("server name")},
	}
	// Run serially because the subtests replace the listener seam.
	for _, tt := range tests {
		// Verify one validation failure.
		t.Run(tt.name, func(t *testing.T) {
			// Track attempts to call the listener from the server goroutine.
			var listened atomic.Bool
			// Prevent accidental socket use if validation regresses.
			t.Cleanup(lpsrv.SetListenAndServe(func(*http.Server) error {
				// Mark the unexpected listener call.
				listened.Store(true)
				// Return a distinguishable error if the listener is reached.
				return listenerErr
			}))
			// Attempt to start the server with invalid inputs.
			err := lpsrv.RunContext(tt.ctx, tt.handler, tt.opts...)
			// Report any listener call before the validation error was returned.
			if listened.Load() {
				// Reject unexpected listener startup.
				t.Error("RunContext invoked the listener for invalid inputs")
			}
			// Check that the expected validation error was returned.
			checkServerInputError(t, "RunContext", err, tt.wantErr)
		})
	}
}

// checkServerInputError verifies that got has the expected validation diagnostic.
func checkServerInputError(t *testing.T, op string, got, want error) {
	// Attribute assertion failures to the calling test.
	t.Helper()
	// Require an error before comparing its message.
	if got == nil {
		// Report the missing validation error.
		t.Fatal(tserr.NilFailed(op))
	}
	// Check that the diagnostic identifies the expected invalid input.
	if got.Error() != want.Error() {
		// Report the actual and expected validation errors.
		t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: op + " error", Actual: got.Error(), Want: want.Error()}))
	}
}

// TestRunSignalLifecycle verifies signal subscription, graceful shutdown, and subscription cleanup.
func TestRunSignalLifecycle(t *testing.T) {
	// Use a distinct startup error to verify that Run preserves the original failure.
	startupErr := errors.New("listener startup failed")
	// Exercise both signal-driven shutdown and startup failure without an OS signal or socket.
	tests := []struct {
		name    string // The subtest name.
		wantErr error  // The expected startup failure, or nil for signal-driven shutdown.
	}{
		{name: "signal triggers graceful shutdown"},
		{name: "startup failure releases subscription", wantErr: startupErr},
	}
	// Run serially because the subtests replace package-level seams.
	for _, tt := range tests {
		// Exercise one Run lifecycle.
		t.Run(tt.name, func(t *testing.T) {
			// Set a predictable port without binding it.
			t.Setenv("PORT", "8080")
			// Hold the listener open until graceful shutdown starts.
			shutdownStarted := make(chan struct{})
			// Keep the simulated signal cancellation available to the listener.
			var signalCancel context.CancelFunc
			// Track release of the signal subscription.
			stops := 0
			// Verify signal wiring without registering real signal handlers.
			t.Cleanup(lpsrv.SetNotifyContext(func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
				// Expect both interactive interruption and Cloud Run termination.
				wantSignals := []os.Signal{os.Interrupt, syscall.SIGTERM}
				// Check the list passed to the signal subscription.
				if !slices.Equal(signals, wantSignals) {
					// Report the actual and expected signal lists.
					t.Errorf("signal subscription = %v, want %v", signals, wantSignals)
				}
				// Derive a signal context that the listener can cancel.
				ctx, cancel := context.WithCancel(parent)
				// Ensure cleanup even if Run fails to release the subscription.
				t.Cleanup(cancel)
				// Expose cancellation to the fake listener.
				signalCancel = cancel
				// Return the context and record calls to its stop function.
				return ctx, func() {
					// Record that Run released the signal subscription.
					stops++
					// Cancel the signal context to release its resources.
					cancel()
				}
			}))
			// Replace the listener so no network socket is opened.
			t.Cleanup(lpsrv.SetListenAndServe(func(*http.Server) error {
				// Simulate an immediate startup failure when requested.
				if tt.wantErr != nil {
					// Return the startup failure without canceling the signal context.
					return tt.wantErr
				}
				// Simulate a signal only after the listener has started.
				signalCancel()
				// Wait until Run asks the server to drain.
				<-shutdownStarted
				// Report normal listener closure.
				return http.ErrServerClosed
			}))
			// Track graceful shutdown calls.
			shutdowns := 0
			// Replace graceful shutdown with a socket-free result.
			t.Cleanup(lpsrv.SetShutdownServer(func(ctx context.Context, _ *http.Server) error {
				// Record the drain attempt.
				shutdowns++
				// Verify that cancellation did not propagate to the drain context.
				if err := ctx.Err(); err != nil {
					// Report the unexpected cancellation of graceful shutdown.
					t.Error(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "shutdown context", Err: err}))
				}
				// Release the listener so Run can collect its result.
				close(shutdownStarted)
				// Report successful draining.
				return nil
			}))
			// Run the signal-aware server with the mocked lifecycle.
			err := lpsrv.Run(http.NotFoundHandler())
			// Require one release regardless of the outcome.
			if stops != 1 {
				// Report an incorrect number of signal subscription releases.
				t.Errorf("signal stop called %d times, want 1", stops)
			}
			// Expect a drain only after simulated signal delivery.
			wantShutdowns := 0
			// Check the outcome of the signal-driven path separately from startup failure.
			if tt.wantErr == nil {
				// Require one graceful shutdown after a signal.
				wantShutdowns = 1
				// Reject errors from the successful mock lifecycle.
				if err != nil {
					// Report the unexpected lifecycle error.
					t.Error(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "Run", Err: err}))
				}
			} else if !errors.Is(err, tt.wantErr) {
				// Report a missing or incorrect startup failure.
				t.Errorf("Run error = %v, want it to wrap %v", err, tt.wantErr)
			}
			// Verify whether Run entered graceful shutdown.
			if shutdowns != wantShutdowns {
				// Report a missing or unexpected drain attempt.
				t.Errorf("Shutdown called %d times, want %d", shutdowns, wantShutdowns)
			}
		})
	}
}

// TestRunContextListenerExit verifies that cancellation does not hide a listener failure.
func TestRunContextListenerExit(t *testing.T) {
	// Use a distinct failure to check that RunContext preserves the listener's error.
	listenerErr := errors.New("listener failed")
	// Exercise listener results both before cancellation and during graceful shutdown.
	tests := []struct {
		name        string // The subtest name.
		result      error  // The result returned by the mock listener.
		cancelFirst bool   // Whether to start shutdown before returning the listener result.
		wantError   bool   // Whether RunContext should return an error.
	}{
		{name: "listener fails before cancellation", result: listenerErr, wantError: true},
		{name: "listener fails after cancellation", result: listenerErr, cancelFirst: true, wantError: true},
		{name: "listener closes after cancellation", result: fmt.Errorf("serve: %w", http.ErrServerClosed), cancelFirst: true},
		{name: "listener returns nil after cancellation", cancelFirst: true, wantError: true},
	}
	// Run serially because each case replaces package-level lifecycle seams.
	for _, tt := range tests {
		// Exercise one scripted listener exit without opening a socket.
		t.Run(tt.name, func(t *testing.T) {
			// Set a predictable port without a fallback warning.
			t.Setenv("PORT", "8080")
			// Create a lifecycle context that the listener can cancel.
			ctx, cancel := context.WithCancel(context.Background())
			// Release context resources when the case finishes.
			defer cancel()
			// Hold the listener result until RunContext has entered shutdown.
			shutdownStarted := make(chan struct{})
			// Install the mock listener.
			t.Cleanup(lpsrv.SetListenAndServe(func(*http.Server) error {
				// Force cancellation to win the select in cases that exercise shutdown.
				if tt.cancelFirst {
					// Cancel the lifecycle before making a listener result available.
					cancel()
					// Wait until the selected cancellation path actually calls shutdown.
					<-shutdownStarted
				}
				// Publish the scripted listener result.
				return tt.result
			}))
			// Script a successful graceful shutdown.
			t.Cleanup(lpsrv.SetShutdownServer(func(context.Context, *http.Server) error {
				// Unblock the listener once the cancellation path starts shutdown.
				close(shutdownStarted)
				// Report successful draining.
				return nil
			}))
			// Run the lifecycle without invoking the handler.
			err := lpsrv.RunContext(ctx, http.NotFoundHandler())
			// Fail if an expected listener failure was dropped.
			if tt.wantError && err == nil {
				// Report the missing error.
				t.Fatal(tserr.NilFailed("RunContext"))
			}
			// Fail if the expected listener closure was reported as an error.
			if !tt.wantError && err != nil {
				// Report the unexpected error.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "RunContext", Err: err}))
			}
			// Check that the structured error retains the original listener failure.
			if tt.wantError && tt.result != nil && !errors.Is(err, tt.result) {
				// Report which failure was lost.
				t.Errorf("RunContext error = %v, want it to wrap %v", err, tt.result)
			}
		})
	}
}

// TestRunContextForcedShutdown verifies that only shutdown timeouts trigger forced closure.
func TestRunContextForcedShutdown(t *testing.T) {
	// Simulate a non-timeout shutdown failure and a failure to force-close connections.
	shutdownErr := errors.New("listener close failed during shutdown")
	// Use a distinct error to verify that it does not replace the timeout.
	closeErr := errors.New("forced close failed")
	// Cover successful shutdown, timeouts with and without close failures, and other errors.
	tests := []struct {
		name        string // The subtest name.
		shutdownErr error  // The error returned by graceful shutdown.
		closeErr    error  // The error returned by forced shutdown.
		wantCloses  int    // The expected number of forced close calls.
	}{
		{name: "graceful shutdown"},
		{name: "shutdown timeout", shutdownErr: context.DeadlineExceeded, wantCloses: 1},
		{name: "wrapped timeout with close failure", shutdownErr: fmt.Errorf("drain: %w", context.DeadlineExceeded), closeErr: closeErr, wantCloses: 1},
		{name: "other shutdown failure", shutdownErr: shutdownErr},
	}
	// Exercise each outcome without binding a network socket.
	for _, tt := range tests {
		// Run serially because the test replaces package-level lifecycle seams.
		t.Run(tt.name, func(t *testing.T) {
			// Set a predictable listening address without a fallback warning.
			t.Setenv("PORT", "8080")
			// Create a lifecycle context for the mock listener to cancel.
			ctx, cancel := context.WithCancel(context.Background())
			// Release the lifecycle context when the subtest exits.
			defer cancel()
			// Hold the listener open until RunContext enters the shutdown path.
			shutdownStarted := make(chan struct{})
			// Cancel from the listener so its seam has been read before cleanup can restore it.
			t.Cleanup(lpsrv.SetListenAndServe(func(*http.Server) error {
				// Trigger graceful shutdown once the mock listener starts.
				cancel()
				// Wait for graceful shutdown before reporting listener closure.
				<-shutdownStarted
				// Report normal listener closure without opening a socket.
				return http.ErrServerClosed
			}))
			// Record which server was passed to graceful shutdown.
			var shuttingDown *http.Server
			// Script the outcome of graceful shutdown.
			t.Cleanup(lpsrv.SetShutdownServer(func(_ context.Context, srv *http.Server) error {
				// Track the server so forced closure can be checked against it.
				shuttingDown = srv
				// Release the listener now that shutdown has started.
				close(shutdownStarted)
				// Return the case's simulated graceful shutdown result.
				return tt.shutdownErr
			}))
			// Count forced close calls before RunContext returns.
			closes := 0
			// Replace the forced close operation with a socket-free check.
			t.Cleanup(lpsrv.SetCloseServer(func(srv *http.Server) error {
				// Verify that forced closure follows graceful shutdown on the same server.
				if srv != shuttingDown {
					// Report an unexpected server or premature forced closure.
					t.Errorf("Close called with %p, want %p from Shutdown", srv, shuttingDown)
				}
				// Record the forced close attempt.
				closes++
				// Return the case's simulated forced close result.
				return tt.closeErr
			}))
			// Run the lifecycle with a handler that the mock listener never invokes.
			err := lpsrv.RunContext(ctx, http.NotFoundHandler())
			// Check that only timeouts force the server closed.
			if closes != tt.wantCloses {
				// Report the actual and expected number of forced close attempts.
				t.Errorf("Close called %d times, want %d", closes, tt.wantCloses)
			}
			// Fail on an unexpected shutdown error in the successful case.
			if tt.shutdownErr == nil && err != nil {
				// Report the error that prevented graceful shutdown.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "RunContext", Err: err}))
			}
			// Require the original shutdown error even when forced closure fails.
			if tt.shutdownErr != nil {
				// Distinguish a missing error from an error that lost the shutdown cause.
				if err == nil {
					// Report the missing shutdown error.
					t.Fatal(tserr.NilFailed("RunContext"))
				}
				// Match the cause through the structured shutdown error.
				if !errors.Is(err, tt.shutdownErr) {
					// Report which cause the returned error should preserve.
					t.Errorf("RunContext error = %v, want it to wrap %v", err, tt.shutdownErr)
				}
			}
		})
	}
}

// TestNewHTTPServerRequestTimeout verifies handler context deadlines and the write timeout buffer.
func TestNewHTTPServerRequestTimeout(t *testing.T) {
	// Define cases for the default, an override, and an earlier incoming deadline.
	tests := []struct {
		name          string         // The subtest name.
		opts          []lpsrv.Option // The server configuration overrides.
		wantTimeout   time.Duration  // The expected request timeout configured on the server.
		wantWrite     time.Duration  // The expected socket write timeout.
		parentTimeout time.Duration  // The incoming request timeout, or zero for no deadline.
	}{
		{name: "default", wantTimeout: 30 * time.Second, wantWrite: 45 * time.Second},
		{name: "configured", opts: []lpsrv.Option{lpsrv.WithRequestTimeout(8 * time.Second), lpsrv.WithWriteTimeoutBuffer(2 * time.Second)}, wantTimeout: 8 * time.Second, wantWrite: 10 * time.Second},
		{name: "earlier incoming deadline", opts: []lpsrv.Option{lpsrv.WithRequestTimeout(8 * time.Second)}, wantTimeout: 8 * time.Second, wantWrite: 23 * time.Second, parentTimeout: 4 * time.Second},
	}
	// Exercise the constructed handler without opening a listener.
	for _, tt := range tests {
		// Verify one request and its server's write timeout.
		t.Run(tt.name, func(t *testing.T) {
			// Capture the deadline seen by the downstream handler.
			var deadline time.Time
			// Define a handler that records its context deadline and writes a response.
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Record the deadline, leaving the zero time if none is set.
				deadline, _ = r.Context().Deadline()
				// Confirm the wrapped handler can still write its response.
				w.WriteHeader(http.StatusNoContent)
			})
			// Construct the server with the case's timeout settings.
			srv, err := lpsrv.NewHTTPServer(":8080", handler, tt.opts...)
			// Stop if the valid settings were rejected.
			if err != nil {
				// Report the unexpected construction error.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "NewHTTPServer", Err: err}))
			}
			// Check that the socket write timeout keeps its configured buffer.
			if srv.WriteTimeout != tt.wantWrite {
				// Report the actual and expected write timeouts.
				t.Errorf("WriteTimeout = %v, want %v", srv.WriteTimeout, tt.wantWrite)
			}
			// Create an in-memory request without a body.
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			// Record the response without opening a socket.
			w := httptest.NewRecorder()
			// Attach an earlier deadline when the case supplies one.
			if tt.parentTimeout != 0 {
				// Derive the incoming deadline from the original request context.
				ctx, cancel := context.WithTimeout(r.Context(), tt.parentTimeout)
				// Release the incoming context timer when the subtest returns.
				defer cancel()
				// Pass the earlier deadline to the server handler.
				r = r.WithContext(ctx)
			}
			// Record a lower bound for when the middleware computes the deadline.
			before := time.Now()
			// Dispatch directly to the constructed server's wrapped handler.
			srv.Handler.ServeHTTP(w, r)
			// Record an upper bound for when the middleware computes the deadline.
			after := time.Now()
			// Ensure the handler's response reaches the caller unchanged.
			if w.Code != http.StatusNoContent {
				// Report the actual and expected status codes.
				t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
			}
			// Ensure the handler observed a request context deadline.
			if deadline.IsZero() {
				// Fail before comparing a missing deadline.
				t.Fatal("handler did not receive a request context deadline")
			}
			// Expect the configured budget measured within the handler invocation.
			earliest, latest := before.Add(tt.wantTimeout), after.Add(tt.wantTimeout)
			// Require an earlier incoming deadline to be preserved exactly.
			if parentDeadline, ok := r.Context().Deadline(); ok {
				// Narrow both bounds to the incoming deadline.
				earliest, latest = parentDeadline, parentDeadline
			}
			// Compare the handler deadline without waiting for it to expire.
			if deadline.Before(earliest) || deadline.After(latest) {
				// Report the observed deadline and the expected bounds.
				t.Errorf("handler deadline = %v, want between %v and %v", deadline, earliest, latest)
			}
		})
	}
}
