// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"context"   // Import the context package for lifecycle cancellation and shutdown deadlines.
	"errors"    // Import the errors package for matching listener closure and shutdown timeouts.
	"fmt"       // Import the fmt package for formatted lifecycle log messages.
	"net/http"  // Import the net/http package for http.Server construction and lifecycle methods.
	"os"        // Import the os package for reading environment variables and OS signals.
	"os/signal" // Import the os/signal package for SIGINT and SIGTERM notifications.
	"strings"   // Import the strings package for trimming environment variable values.
	"syscall"   // Import the syscall package for SIGTERM handling on Cloud Run scale-down.

	"github.com/thorsphere/tserr" // Import the tserr package for structured error construction.
	"github.com/thorsphere/tslog" // Import the tslog package for structured logging.
)

// The following constants define environment variable names and bind prefixes for Cloud Run servers.
const (
	portEnv           = "PORT" // The environment variable supplying the TCP listen port on Google Cloud Run.
	defaultHostPrefix = ":"    // The host prefix binding the HTTP listener to all network interfaces.
)

// notifyContext returns a copy of parent canceled when any signal in signals
// arrives or when the returned stop function is invoked. It is the signal seam,
// swapped via SetNotifyContext in export_test.go so unit tests trigger shutdown
// without sending real OS signals.
var notifyContext = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
	// Delegate to the standard library OS signal context helper.
	return signal.NotifyContext(parent, signals...)
}

// listenAndServe binds srv to its configured address and serves incoming HTTP
// requests. It is the listener seam, swapped via SetListenAndServe in
// export_test.go so unit tests never open a real network socket.
var listenAndServe = func(srv *http.Server) error {
	// Delegate to the standard library HTTP server listener.
	return srv.ListenAndServe()
}

// shutdownServer gracefully stops srv from accepting new connections and drains
// active requests within ctx. It is the shutdown seam, swapped via
// SetShutdownServer in export_test.go so unit tests script shutdown outcomes
// without a network socket.
var shutdownServer = func(ctx context.Context, srv *http.Server) error {
	// Delegate to the standard library HTTP server graceful shutdown.
	return srv.Shutdown(ctx)
}

// closeServer immediately closes srv's listeners and active HTTP connections.
// It is swapped via SetCloseServer in export_test.go for socket-free tests.
var closeServer = func(srv *http.Server) error {
	// Delegate to the standard library HTTP server forced shutdown.
	return srv.Close()
}

// RequireEnv returns the value of the environment variable named key, or an
// error if key is empty or whitespace-only or if the environment variable is
// unset or whitespace-only.
func RequireEnv(key string) (string, error) {
	// Trim surrounding whitespace from the environment variable name.
	trimmedKey := strings.TrimSpace(key)
	// Reject blank environment variable names before lookup.
	if trimmedKey == "" {
		// Return an empty-name error when key is blank.
		return "", tserr.Empty("environment variable name")
	}
	// Read the raw environment variable value from the process environment.
	val := os.Getenv(trimmedKey)
	// Fail fast when the variable is unset or whitespace-only.
	if strings.TrimSpace(val) == "" {
		// Return a not-set error naming the missing environment variable.
		return "", tserr.NotSet(trimmedKey)
	}
	// Return the environment variable value and nil error.
	return val, nil
}

// resolveListenAddr returns the wildcard TCP listen address built from the PORT
// environment variable, falling back to DefaultPort with a warning log when
// PORT is unset, whitespace-only, or a bare colon.
func resolveListenAddr() string {
	// Read and trim the listen port from the environment (Cloud Run injects PORT).
	port := strings.TrimSpace(os.Getenv(portEnv))
	// Fall back to DefaultPort when PORT is unset or whitespace-only.
	if port == "" {
		// Warn that Cloud Run should supply PORT in production environments.
		tslog.Warn(fmt.Sprintf("%s environment variable not set, defaulting to %s", portEnv, defaultPort))
		// Use the fallback development port.
		port = defaultPort
	}
	// Strip an optional leading colon in case PORT was configured as ":8080".
	port = strings.TrimPrefix(port, defaultHostPrefix)
	// Guard against a bare ":" value that leaves no port number after trimming.
	if port == "" {
		// Warn that PORT carried no port digits and fall back to DefaultPort.
		tslog.Warn(fmt.Sprintf("%s environment variable invalid, defaulting to %s", portEnv, defaultPort))
		// Use the fallback development port.
		port = defaultPort
	}
	// Construct and return the wildcard listen address across all interfaces.
	return defaultHostPrefix + port
}

// newServerFromConfig returns an http.Server bound to addr with socket timeouts
// from cfg and handler wrapped to apply the request context deadline.
func newServerFromConfig(addr string, handler http.Handler, cfg *serverConfig) *http.Server {
	// Construct the server with a deadline-bounded handler and a buffered WriteTimeout.
	return &http.Server{
		Addr:              addr,
		Handler:           TimeoutMiddleware(cfg.requestTimeout, handler),
		ReadHeaderTimeout: cfg.readHeaderTimeout,
		ReadTimeout:       cfg.readTimeout,
		WriteTimeout:      cfg.requestTimeout + cfg.writeTimeoutBuffer,
		IdleTimeout:       cfg.idleTimeout,
	}
}

// NewHTTPServer returns an http.Server bound to addr and handler with a request
// context deadline and hardened socket timeouts configured from opts,
// or an error if addr is blank, handler is nil, or any option is invalid.
func NewHTTPServer(addr string, handler http.Handler, opts ...Option) (*http.Server, error) {
	// Trim surrounding whitespace from the target listen address.
	trimmedAddr := strings.TrimSpace(addr)
	// Reject empty or whitespace-only listen addresses.
	if trimmedAddr == "" {
		// Return an empty-address error when addr is blank.
		return nil, tserr.Empty("address")
	}
	// Reject a nil HTTP handler before building the server.
	if handler == nil {
		// Return an empty-handler error when handler is nil.
		return nil, tserr.Empty("handler")
	}
	// Resolve package defaults and caller-supplied options.
	cfg, err := resolveConfig(opts...)
	// Propagate any option validation failure.
	if err != nil {
		// Return nil server and the configuration error.
		return nil, err
	}
	// Return the configured HTTP server and nil error.
	return newServerFromConfig(trimmedAddr, handler, cfg), nil
}

// Run starts an HTTP server for handler configured with opts on the Cloud Run
// PORT environment variable, listens for os.Interrupt and syscall.SIGTERM, and
// gracefully drains active requests upon signal receipt.
func Run(handler http.Handler, opts ...Option) error {
	// Subscribe to SIGINT and SIGTERM so container shutdown cancels the server context.
	ctx, stop := notifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Release signal notification resources when Run returns.
	defer stop()
	// Delegate the server lifecycle to RunContext with the signal-aware context.
	return RunContext(ctx, handler, opts...)
}

// RunContext reads PORT from the environment, serves HTTP requests with handler
// on all interfaces with request context deadlines and socket timeouts configured
// from opts, and gracefully drains in-flight requests when ctx is canceled.
// If the shutdown deadline expires before draining finishes, it force-closes
// active HTTP connections and returns the shutdown error.
// It returns an error if ctx is nil, handler is nil, any option is invalid,
// the listener fails unexpectedly, or graceful shutdown fails.
func RunContext(ctx context.Context, handler http.Handler, opts ...Option) error {
	// Reject a nil context before starting any work.
	if ctx == nil {
		// Return an empty-context error when ctx is nil.
		return tserr.Empty("context")
	}
	// Reject a nil HTTP handler before binding a listener.
	if handler == nil {
		// Return an empty-handler error when handler is nil.
		return tserr.Empty("handler")
	}
	// Resolve package defaults and caller-supplied options before starting the listener.
	cfg, err := resolveConfig(opts...)
	// Propagate any option validation error immediately.
	if err != nil {
		// Return the option configuration failure.
		return err
	}
	// Resolve the wildcard TCP listen address from PORT or DefaultPort.
	serverAddr := resolveListenAddr()
	// Construct the HTTP server with hardened timeouts derived from cfg.
	server := newServerFromConfig(serverAddr, handler, cfg)
	// Buffer the listener's final result so it can exit even if shutdown fails.
	listenerResult := make(chan error, 1)
	// Start the HTTP listener in a background goroutine.
	go func() {
		// Log the server name and address on which the listener is starting.
		tslog.Info(fmt.Sprintf("Starting %s on %v", cfg.name, serverAddr))
		// Serve requests until the listener exits.
		err := listenAndServe(server)
		// Treat only the standard server-closed result as a clean listener exit.
		if errors.Is(err, http.ErrServerClosed) {
			// Report the expected closure to RunContext.
			listenerResult <- nil
			// Finish without reporting a listener failure.
			return
		}
		// Guard against an unexpected nil result from the listener seam.
		if err == nil {
			// Explain why a nil result is not a clean listener exit.
			err = errors.New("listener exited without returning an error")
		}
		// Report the listener failure with the operation and address.
		listenerResult <- tserr.Op(&tserr.OpArgs{Op: "ListenAndServe", Fn: serverAddr, Err: err})
	}()
	// Wait until ctx is canceled or the listener exits.
	select {
	case err := <-listenerResult:
		// Return the listener result if it exits before shutdown starts.
		return err
	case <-ctx.Done():
	}
	// Log that graceful shutdown has started after context cancellation.
	tslog.Info("Shutting down server gracefully...")
	// Bound the graceful shutdown duration so draining requests cannot hang indefinitely.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	// Release the shutdown context timer when RunContext returns.
	defer cancel()
	// Gracefully stop accepting new connections and drain in-flight requests.
	if err := shutdownServer(shutdownCtx, server); err != nil {
		// Force-close active connections if graceful shutdown ran out of time.
		if errors.Is(err, context.DeadlineExceeded) {
			// Attempt to close any connections still open after the drain deadline.
			if closeErr := closeServer(server); closeErr != nil {
				// Log the close failure without replacing the original shutdown error.
				tslog.Error(tserr.Op(&tserr.OpArgs{Op: "Close", Fn: serverAddr, Err: closeErr}))
			}
		}
		// Return the wrapped shutdown failure when draining does not complete cleanly.
		return tserr.Op(&tserr.OpArgs{Op: "Shutdown", Fn: serverAddr, Err: err})
	}
	// Collect the listener result after shutdown so cancellation cannot hide a failure.
	if err := <-listenerResult; err != nil {
		// Return the listener failure instead of reporting a clean exit.
		return err
	}
	// Log that the server has exited cleanly.
	tslog.Info("Server successfully exited")
	// Return nil to indicate a clean server lifecycle.
	return nil
}
