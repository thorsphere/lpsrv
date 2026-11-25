// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"errors"  // Import the errors package for constructing option validation errors.
	"math"    // Import the math package for overflow checks on derived timeout durations.
	"strings" // Import the strings package for trimming server names.
	"time"    // Import the time package for server timeout durations.

	"github.com/thorsphere/tserr" // Import the tserr package for structured error construction.
)

// The following constants define the Cloud Run HTTP server defaults and timeout budgets.
const (
	defaultPort               = "8080"           // The fallback listen port when the PORT environment variable is unset.
	defaultServerName         = "Server"         // The default server display name written in startup logs.
	defaultReadHeaderTimeout  = 5 * time.Second  // The default maximum duration for reading HTTP request headers.
	defaultReadTimeout        = 15 * time.Second // The default maximum duration for reading the entire HTTP request.
	defaultRequestTimeout     = 30 * time.Second // The default handler request budget from which WriteTimeout is derived.
	defaultWriteTimeoutBuffer = 15 * time.Second // The default safety margin added above RequestTimeout to derive WriteTimeout.
	defaultIdleTimeout        = 60 * time.Second // The default maximum duration to keep idle keep-alive connections open.
	defaultShutdownTimeout    = 8 * time.Second  // The default deadline for draining in-flight requests on shutdown.
)

// serverConfig holds the resolved HTTP server settings and timeout budgets. The
// zero value is not usable directly; construct defaults with defaultConfig.
type serverConfig struct {
	name               string        // The human-readable server name written in lifecycle log messages.
	readHeaderTimeout  time.Duration // The maximum duration allowed to read request headers.
	readTimeout        time.Duration // The maximum duration allowed to read the full request.
	requestTimeout     time.Duration // The per-request handler budget used to derive WriteTimeout.
	writeTimeoutBuffer time.Duration // The grace buffer added to requestTimeout to compute WriteTimeout.
	idleTimeout        time.Duration // The maximum duration idle keep-alive connections remain open.
	shutdownTimeout    time.Duration // The maximum duration allowed for graceful server shutdown.
}

// Option configures HTTP server settings and timeout budgets for NewHTTPServer,
// Run, and RunContext. A nil Option is ignored.
type Option func(*serverConfig) error

// defaultConfig returns a serverConfig populated with the package default server
// name and Cloud Run timeout budgets.
func defaultConfig() *serverConfig {
	// Construct and return the default server configuration.
	return &serverConfig{
		name:               defaultServerName,
		readHeaderTimeout:  defaultReadHeaderTimeout,
		readTimeout:        defaultReadTimeout,
		requestTimeout:     defaultRequestTimeout,
		writeTimeoutBuffer: defaultWriteTimeoutBuffer,
		idleTimeout:        defaultIdleTimeout,
		shutdownTimeout:    defaultShutdownTimeout,
	}
}

// positiveDurationError returns a structured configuration error indicating
// that the named duration setting must be strictly positive.
func positiveDurationError(setting string) error {
	// Wrap the non-positive duration message with the setting name.
	return tserr.Op(&tserr.OpArgs{
		Op:  "configure server",
		Fn:  setting,
		Err: errors.New("duration must be positive"),
	})
}

// resolveConfig returns a validated serverConfig built by applying opts to the
// package defaults, or an error if any option fails validation or the derived
// WriteTimeout overflows time.Duration.
func resolveConfig(opts ...Option) (*serverConfig, error) {
	// Start from the package default server settings.
	cfg := defaultConfig()
	// Apply each caller-supplied option in order.
	for _, opt := range opts {
		// Skip nil Option values defensively.
		if opt == nil {
			// Continue to the next option.
			continue
		}
		// Apply the option and propagate any validation error.
		if err := opt(cfg); err != nil {
			// Return nil config and the option validation error.
			return nil, err
		}
	}
	// Guard against integer overflow when adding writeTimeoutBuffer to requestTimeout.
	if cfg.requestTimeout > math.MaxInt64-cfg.writeTimeoutBuffer {
		// Return an error rather than wrapping WriteTimeout into a negative duration.
		return nil, tserr.Op(&tserr.OpArgs{
			Op:  "configure server",
			Fn:  "WriteTimeout",
			Err: errors.New("request timeout and write timeout buffer overflow time.Duration"),
		})
	}
	// Return the validated server configuration and nil error.
	return cfg, nil
}

// WithName returns an Option that sets the server display name used in startup
// log messages to name, or returns an error when name is empty or
// whitespace-only.
func WithName(name string) Option {
	// Return a closure that validates and applies name.
	return func(cfg *serverConfig) error {
		// Trim surrounding whitespace from the supplied server name.
		trimmed := strings.TrimSpace(name)
		// Reject empty or whitespace-only server names.
		if trimmed == "" {
			// Return an empty-name error when name is blank.
			return tserr.Empty("server name")
		}
		// Store the validated server display name.
		cfg.name = trimmed
		// Return nil to indicate success.
		return nil
	}
}

// WithReadHeaderTimeout returns an Option that sets the maximum duration for
// reading HTTP request headers to d, or returns an error when d is zero or
// negative.
func WithReadHeaderTimeout(d time.Duration) Option {
	// Return a closure that validates and applies d.
	return func(cfg *serverConfig) error {
		// Reject non-positive header read timeouts.
		if d <= 0 {
			// Return a validation error naming ReadHeaderTimeout.
			return positiveDurationError("ReadHeaderTimeout")
		}
		// Store the validated header read timeout.
		cfg.readHeaderTimeout = d
		// Return nil to indicate success.
		return nil
	}
}

// WithReadTimeout returns an Option that sets the maximum duration for reading
// an entire HTTP request to d, or returns an error when d is zero or negative.
func WithReadTimeout(d time.Duration) Option {
	// Return a closure that validates and applies d.
	return func(cfg *serverConfig) error {
		// Reject non-positive request read timeouts.
		if d <= 0 {
			// Return a validation error naming ReadTimeout.
			return positiveDurationError("ReadTimeout")
		}
		// Store the validated request read timeout.
		cfg.readTimeout = d
		// Return nil to indicate success.
		return nil
	}
}

// WithRequestTimeout returns an Option that sets the request context timeout to d
// and uses d to derive the server's WriteTimeout. The option rejects nonpositive
// durations. An earlier incoming deadline is preserved, and handlers must observe
// context cancellation to stop work when the deadline expires.
func WithRequestTimeout(d time.Duration) Option {
	// Return a closure that validates and applies d.
	return func(cfg *serverConfig) error {
		// Reject non-positive request timeout budgets.
		if d <= 0 {
			// Return a validation error naming RequestTimeout.
			return positiveDurationError("RequestTimeout")
		}
		// Store the validated request timeout budget.
		cfg.requestTimeout = d
		// Return nil to indicate success.
		return nil
	}
}

// WithWriteTimeoutBuffer returns an Option that sets the safety margin added
// above RequestTimeout when computing the server's WriteTimeout to d, or
// returns an error when d is zero or negative.
func WithWriteTimeoutBuffer(d time.Duration) Option {
	// Return a closure that validates and applies d.
	return func(cfg *serverConfig) error {
		// Require a strictly positive buffer so WriteTimeout always exceeds RequestTimeout.
		if d <= 0 {
			// Return a validation error naming WriteTimeoutBuffer.
			return positiveDurationError("WriteTimeoutBuffer")
		}
		// Store the validated write timeout safety buffer.
		cfg.writeTimeoutBuffer = d
		// Return nil to indicate success.
		return nil
	}
}

// WithIdleTimeout returns an Option that sets the maximum duration to keep idle
// keep-alive connections open to d, or returns an error when d is zero or
// negative.
func WithIdleTimeout(d time.Duration) Option {
	// Return a closure that validates and applies d.
	return func(cfg *serverConfig) error {
		// Reject non-positive idle connection timeouts.
		if d <= 0 {
			// Return a validation error naming IdleTimeout.
			return positiveDurationError("IdleTimeout")
		}
		// Store the validated idle timeout.
		cfg.idleTimeout = d
		// Return nil to indicate success.
		return nil
	}
}

// WithShutdownTimeout returns an Option that sets the graceful shutdown drain
// deadline to d, or returns an error when d is zero or negative.
func WithShutdownTimeout(d time.Duration) Option {
	// Return a closure that validates and applies d.
	return func(cfg *serverConfig) error {
		// Reject non-positive graceful shutdown timeouts.
		if d <= 0 {
			// Return a validation error naming ShutdownTimeout.
			return positiveDurationError("ShutdownTimeout")
		}
		// Store the validated shutdown timeout.
		cfg.shutdownTimeout = d
		// Return nil to indicate success.
		return nil
	}
}
