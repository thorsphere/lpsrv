// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv_test

// Import packages
import (
	"math"    // Import the math package for the maximum time.Duration value.
	"strings" // Import the strings package for validation error labels.
	"testing" // Import the testing package for table-driven tests.
	"time"    // Import the time package for timeout values.

	"github.com/thorsphere/lpsrv" // Import the lpsrv package under test.
	"github.com/thorsphere/tserr" // Import the tserr package for assertion errors.
)

// TestServerTimeoutOptions verifies the socket timeouts resolved from valid options.
func TestServerTimeoutOptions(t *testing.T) {
	// Define the socket timeouts visible on the constructed server.
	type socketTimeouts struct {
		readHeader time.Duration // The deadline for reading request headers.
		read       time.Duration // The deadline for reading the full request.
		write      time.Duration // The response write timeout including its buffer.
		idle       time.Duration // The idle keep-alive timeout.
	}
	// Record the expected defaults for cases that do not override them.
	defaults := socketTimeouts{readHeader: 5 * time.Second, read: 15 * time.Second, write: 45 * time.Second, idle: 60 * time.Second}
	// Cover defaults, nil options, valid overrides, precedence, and the exact duration limit.
	tests := []struct {
		name string         // The subtest name.
		opts []lpsrv.Option // The options supplied to NewHTTPServer.
		want socketTimeouts // The expected socket timeouts.
	}{
		{name: "defaults", want: defaults},
		{name: "nil option", opts: []lpsrv.Option{nil}, want: defaults},
		{
			name: "socket timeout overrides",
			opts: []lpsrv.Option{
				lpsrv.WithReadHeaderTimeout(2 * time.Second),
				lpsrv.WithReadTimeout(9 * time.Second),
				lpsrv.WithIdleTimeout(20 * time.Second),
			},
			want: socketTimeouts{readHeader: 2 * time.Second, read: 9 * time.Second, write: defaults.write, idle: 20 * time.Second},
		},
		{
			name: "last valid option wins",
			opts: []lpsrv.Option{lpsrv.WithIdleTimeout(10 * time.Second), nil, lpsrv.WithIdleTimeout(20 * time.Second)},
			want: socketTimeouts{readHeader: defaults.readHeader, read: defaults.read, write: defaults.write, idle: 20 * time.Second},
		},
		{
			name: "write timeout at duration limit",
			opts: []lpsrv.Option{lpsrv.WithRequestTimeout(time.Duration(math.MaxInt64) - time.Nanosecond), lpsrv.WithWriteTimeoutBuffer(time.Nanosecond)},
			want: socketTimeouts{readHeader: defaults.readHeader, read: defaults.read, write: time.Duration(math.MaxInt64), idle: defaults.idle},
		},
	}
	// Check each configuration without opening a listener.
	for _, tt := range tests {
		// Construct and inspect the server for this case.
		t.Run(tt.name, func(t *testing.T) {
			// Apply the options with a valid handler and address.
			srv, err := lpsrv.NewHTTPServer(":8080", lpsrv.HealthHandler(), tt.opts...)
			// Fail if the valid configuration was rejected.
			if err != nil {
				// Report the unexpected construction error.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "NewHTTPServer", Err: err}))
			}
			// Capture all configured socket timeouts.
			got := socketTimeouts{readHeader: srv.ReadHeaderTimeout, read: srv.ReadTimeout, write: srv.WriteTimeout, idle: srv.IdleTimeout}
			// Compare the entire timeout configuration.
			if got != tt.want {
				// Report the observed and expected timeouts.
				t.Errorf("socket timeouts = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestInvalidServerOptions verifies that invalid options prevent server construction.
func TestInvalidServerOptions(t *testing.T) {
	// Define an invalid configuration and the setting its error must identify.
	type invalidOptionCase struct {
		name      string         // The subtest name.
		opts      []lpsrv.Option // The options supplied to NewHTTPServer.
		wantLabel string         // The setting name expected in the error.
	}
	// Cover invalid names, failure before a later override, and overflow by one nanosecond.
	tests := []invalidOptionCase{
		{name: "empty name", opts: []lpsrv.Option{lpsrv.WithName("")}, wantLabel: "server name"},
		{name: "whitespace-only name", opts: []lpsrv.Option{lpsrv.WithName(" \t\n")}, wantLabel: "server name"},
		{name: "invalid option before valid override", opts: []lpsrv.Option{lpsrv.WithIdleTimeout(0), lpsrv.WithIdleTimeout(time.Second)}, wantLabel: "IdleTimeout"},
		{
			name:      "write timeout overflows by one nanosecond",
			opts:      []lpsrv.Option{lpsrv.WithRequestTimeout(time.Duration(math.MaxInt64)), lpsrv.WithWriteTimeoutBuffer(time.Nanosecond)},
			wantLabel: "WriteTimeout",
		},
	}
	// List every duration option that requires a strictly positive value.
	durations := []struct {
		label string                           // The setting name expected in the validation error.
		apply func(time.Duration) lpsrv.Option // The constructor for that duration option.
	}{
		{label: "ReadHeaderTimeout", apply: lpsrv.WithReadHeaderTimeout},
		{label: "ReadTimeout", apply: lpsrv.WithReadTimeout},
		{label: "RequestTimeout", apply: lpsrv.WithRequestTimeout},
		{label: "WriteTimeoutBuffer", apply: lpsrv.WithWriteTimeoutBuffer},
		{label: "IdleTimeout", apply: lpsrv.WithIdleTimeout},
		{label: "ShutdownTimeout", apply: lpsrv.WithShutdownTimeout},
	}
	// Add separate cases for zero and negative values for each duration option.
	for _, d := range durations {
		// Append both invalid values with the expected setting name.
		tests = append(tests,
			invalidOptionCase{name: d.label + "/zero", opts: []lpsrv.Option{d.apply(0)}, wantLabel: d.label},
			invalidOptionCase{name: d.label + "/negative", opts: []lpsrv.Option{d.apply(-time.Nanosecond)}, wantLabel: d.label},
		)
	}
	// Assert that each invalid configuration fails before returning a server.
	for _, tt := range tests {
		// Exercise a single invalid configuration.
		t.Run(tt.name, func(t *testing.T) {
			// Apply the invalid options with a valid handler and address.
			srv, err := lpsrv.NewHTTPServer(":8080", lpsrv.HealthHandler(), tt.opts...)
			// Require a nil server when construction fails.
			if srv != nil {
				// Report that an invalid configuration produced a server.
				t.Error("NewHTTPServer returned a non-nil server for invalid options")
			}
			// Require a validation error before inspecting its text.
			if err == nil {
				// Report the missing validation error.
				t.Fatal(tserr.NilFailed("NewHTTPServer"))
			}
			// Check the setting name without depending on the full error format.
			if !strings.Contains(err.Error(), tt.wantLabel) {
				// Report the unexpected error label.
				t.Errorf("NewHTTPServer error = %q, want it to identify %q", err, tt.wantLabel)
			}
		})
	}
}

// TestLifecycleOptions verifies the resolved display name and graceful shutdown deadline.
func TestLifecycleOptions(t *testing.T) {
	// Cover the defaults and successful overrides for lifecycle-only settings.
	tests := []struct {
		name         string         // The subtest name.
		opts         []lpsrv.Option // The options supplied to the resolver.
		wantName     string         // The expected server display name.
		wantShutdown time.Duration  // The expected graceful shutdown timeout.
	}{
		{name: "defaults", wantName: "Server", wantShutdown: 8 * time.Second},
		{name: "trimmed name", opts: []lpsrv.Option{lpsrv.WithName(" \tAPI Server\n ")}, wantName: "API Server", wantShutdown: 8 * time.Second},
		{name: "shutdown override", opts: []lpsrv.Option{lpsrv.WithShutdownTimeout(2 * time.Second)}, wantName: "Server", wantShutdown: 2 * time.Second},
	}
	// Resolve lifecycle settings without starting or stopping a server.
	for _, tt := range tests {
		// Check both resolved settings for this case.
		t.Run(tt.name, func(t *testing.T) {
			// Read only the lifecycle fields not exposed by NewHTTPServer.
			name, shutdown, err := lpsrv.ResolveLifecycleOptions(tt.opts...)
			// Stop if the resolver rejected valid options.
			if err != nil {
				// Report the unexpected configuration error.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "ResolveLifecycleOptions", Err: err}))
			}
			// Check the resolved display name.
			if name != tt.wantName {
				// Report the actual and expected names.
				t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "server name", Actual: name, Want: tt.wantName}))
			}
			// Check the resolved shutdown timeout.
			if shutdown != tt.wantShutdown {
				// Report the actual and expected shutdown timeouts.
				t.Errorf("shutdown timeout = %v, want %v", shutdown, tt.wantShutdown)
			}
		})
	}
}
