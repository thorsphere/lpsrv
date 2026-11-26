// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"net/http" // Import the net/http package for request path assertions.
	"time"     // Import the time package for the resolved shutdown timeout.
)

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
