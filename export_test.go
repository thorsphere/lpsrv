// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"context"  // Import the context package for shutdown deadlines.
	"net/http" // Import the net/http package for request paths and lifecycle seams.
	"os"       // Import the os package for signal notification seams.
	"time"     // Import the time package for the resolved shutdown timeout.
)

// SetNotifyContext replaces the signal subscription seam and returns a function to restore it.
func SetNotifyContext(fn func(context.Context, ...os.Signal) (context.Context, context.CancelFunc)) func() {
	// Keep the original signal subscription for test cleanup.
	previous := notifyContext
	// Install the test signal subscription.
	notifyContext = fn
	// Return a cleanup function that reinstates the original subscription.
	return func() {
		// Restore the signal subscription seam.
		notifyContext = previous
	}
}

// SetListenAndServe replaces the listener seam and returns a function to restore it.
func SetListenAndServe(fn func(*http.Server) error) func() {
	// Keep the original listener for test cleanup.
	previous := listenAndServe
	// Install the test listener.
	listenAndServe = fn
	// Return a cleanup function that reinstates the original listener.
	return func() {
		// Restore the listener seam.
		listenAndServe = previous
	}
}

// SetShutdownServer replaces the graceful shutdown seam and returns a function to restore it.
func SetShutdownServer(fn func(context.Context, *http.Server) error) func() {
	// Keep the original graceful shutdown for test cleanup.
	previous := shutdownServer
	// Install the test shutdown function.
	shutdownServer = fn
	// Return a cleanup function that reinstates the original graceful shutdown.
	return func() {
		// Restore the shutdown seam.
		shutdownServer = previous
	}
}

// SetCloseServer replaces the forced shutdown seam and returns a function to restore it.
func SetCloseServer(fn func(*http.Server) error) func() {
	// Keep the original forced shutdown for test cleanup.
	previous := closeServer
	// Install the test close function.
	closeServer = fn
	// Return a cleanup function that reinstates the original forced shutdown.
	return func() {
		// Restore the close seam.
		closeServer = previous
	}
}

// RequestPath returns the path that error diagnostics would use for r.
func RequestPath(r *http.Request) string {
	// Expose the private path helper to external tests.
	return requestPath(r)
}

// ResolveLifecycleOptions returns the server name and shutdown timeout resolved
// from opts, or an error if the options do not produce a valid configuration.
func ResolveLifecycleOptions(opts ...Option) (string, time.Duration, error) {
	// Apply and validate the options using the production resolver.
	cfg, err := resolveConfig(opts...)
	// Return zero values if configuration failed.
	if err != nil {
		// Propagate the validation error to the test caller.
		return "", 0, err
	}
	// Expose only the lifecycle fields that NewHTTPServer does not expose.
	return cfg.name, cfg.shutdownTimeout, nil
}
