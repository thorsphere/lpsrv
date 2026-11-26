// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv_test

// Import packages
import (
	"net/http"          // Import the net/http package for HTTP handlers and status codes.
	"net/http/httptest" // Import the net/http/httptest package for socket-free requests and recorders.
	"strings"           // Import the strings package to check that validation errors omit secrets.
	"testing"           // Import the testing package for table-driven tests.

	"github.com/thorsphere/lpsrv" // Import the lpsrv package under test.
	"github.com/thorsphere/tserr" // Import the tserr package for error assertions.
)

// TestValidateBearerToken verifies that the validator and middleware agree on configured token syntax.
func TestValidateBearerToken(t *testing.T) {
	// Define valid credentials and malformed configurations to reject.
	tests := []struct {
		name  string // The subtest name.
		token string // The configured token and credential value.
		valid bool   // Whether the token satisfies RFC 6750.
	}{
		{name: "single character", token: "A", valid: true},
		{name: "supported alphabet", token: "Az09-._~+/", valid: true},
		{name: "trailing padding", token: "Az09-._~+/===", valid: true},
		{name: "empty"},
		{name: "whitespace only", token: " \t"},
		{name: "embedded space", token: "secret value"},
		{name: "leading space", token: " secret"},
		{name: "trailing space", token: "secret "},
		{name: "tab", token: "secret\tvalue"},
		{name: "non-ASCII", token: "sécret"},
		{name: "comma", token: "secret,value"},
		{name: "padding only", token: "=="},
		{name: "embedded padding", token: "secret=value=="},
	}
	// Exercise both entry points for each configured token.
	for _, tt := range tests {
		// Check validation and the middleware response together.
		t.Run(tt.name, func(t *testing.T) {
			// Validate the configured token directly.
			err := lpsrv.ValidateBearerToken(tt.token)
			// Require successful validation only for a syntactically valid token.
			if tt.valid && err != nil {
				// Report the unexpected validation error.
				t.Fatal(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "ValidateBearerToken", Err: err}))
			}
			// Require an error when validation rejects the configuration.
			if !tt.valid && err == nil {
				// Report the missing validation error.
				t.Fatal(tserr.NilFailed("ValidateBearerToken"))
			}
			// Check that the error does not echo the malformed secret.
			if err != nil && strings.TrimSpace(tt.token) != "" && strings.Contains(err.Error(), tt.token) {
				// Fail without printing either the secret or the error containing it.
				t.Fatal("validation error exposes the token")
			}
			// Mark forwarded requests with a distinct status.
			handler := lpsrv.RequireBearerAuth(tt.token, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// Indicate that authentication succeeded.
				w.WriteHeader(http.StatusNoContent)
			}))
			// Present the configured value verbatim, including when it is malformed.
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			// Supply a single Authorization header.
			r.Header.Set("Authorization", "Bearer "+tt.token)
			// Capture and run the middleware response without a listener.
			w := httptest.NewRecorder()
			// Authenticate the request.
			handler.ServeHTTP(w, r)
			// Expect invalid configurations to fail closed.
			wantStatus := http.StatusUnauthorized
			// Expect a matching and valid token to reach the downstream handler.
			if tt.valid {
				// Match the status written by the downstream handler.
				wantStatus = http.StatusNoContent
			}
			// Verify the recorded status and read the committed headers.
			resp := recordedHandlerResponse(t, w, wantStatus)
			// Require the Bearer challenge on invalid configurations.
			if !tt.valid {
				// Compare the committed challenge with the expected scheme.
				checkHandlerString(t, "WWW-Authenticate", resp.Header.Get("WWW-Authenticate"), "Bearer")
			}
		})
	}
}

// TestRequireBearerAuthMissingHandler verifies that missing handlers return a sanitized error only after authentication.
func TestRequireBearerAuthMissingHandler(t *testing.T) {
	// Define cases for both forms of nil and for authentication failure.
	tests := []struct {
		name       string       // The subtest name.
		next       http.Handler // The missing downstream handler.
		credential string       // The token supplied with the request.
		wantStatus int          // The expected HTTP status.
	}{
		{name: "nil handler interface", credential: "Token", wantStatus: http.StatusInternalServerError},
		{name: "nil handler function", next: http.HandlerFunc(nil), credential: "Token", wantStatus: http.StatusInternalServerError},
		{name: "nil handler function without valid credentials", next: http.HandlerFunc(nil), credential: "Wrong", wantStatus: http.StatusUnauthorized},
	}
	// Exercise both missing-handler paths without a listener.
	for _, tt := range tests {
		// Check the response for the supplied credential and handler.
		t.Run(tt.name, func(t *testing.T) {
			// Build a request with the case's Bearer credential.
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			// Supply the credential as a single Authorization header.
			r.Header.Set("Authorization", "Bearer "+tt.credential)
			// Record the response directly without server panic recovery.
			w := httptest.NewRecorder()
			// Invoke the middleware with the missing downstream handler.
			lpsrv.RequireBearerAuth("Token", tt.next).ServeHTTP(w, r)
			// Verify the status and read the committed headers.
			resp := recordedHandlerResponse(t, w, tt.wantStatus)
			// Verify that the response body contains only the standard status text.
			checkHandlerString(t, "response body", w.Body.String(), http.StatusText(tt.wantStatus)+"\n")
			// Check that failed authentication still issues the Bearer challenge.
			if tt.wantStatus == http.StatusUnauthorized {
				// Require the challenge on a 401 instead of reporting the missing handler.
				checkHandlerString(t, "WWW-Authenticate", resp.Header.Get("WWW-Authenticate"), "Bearer")
			}
		})
	}
}

// TestRequireBearerAuthScheme verifies that only the Bearer scheme is case-insensitive and that its separator uses ASCII spaces.
func TestRequireBearerAuthScheme(t *testing.T) {
	// Define accepted credentials and inputs that must remain unauthorized.
	tests := []struct {
		name       string   // The subtest name.
		headers    []string // The Authorization header values to supply.
		wantStatus int      // The expected HTTP status.
	}{
		{name: "canonical scheme", headers: []string{"Bearer Token"}, wantStatus: http.StatusNoContent},
		{name: "lowercase scheme", headers: []string{"bearer Token"}, wantStatus: http.StatusNoContent},
		{name: "mixed case and multiple spaces", headers: []string{"bEaReR   Token"}, wantStatus: http.StatusNoContent},
		{name: "token case mismatch", headers: []string{"Bearer token"}, wantStatus: http.StatusUnauthorized},
		{name: "missing separator", headers: []string{"BearerToken"}, wantStatus: http.StatusUnauthorized},
		{name: "tab separator", headers: []string{"Bearer\tToken"}, wantStatus: http.StatusUnauthorized},
		{name: "tab after space", headers: []string{"Bearer \tToken"}, wantStatus: http.StatusUnauthorized},
		{name: "incorrect scheme", headers: []string{"Basic Token"}, wantStatus: http.StatusUnauthorized},
		{name: "missing token", headers: []string{"Bearer  "}, wantStatus: http.StatusUnauthorized},
		{name: "missing header", wantStatus: http.StatusUnauthorized},
		{name: "duplicate headers", headers: []string{"Bearer Token", "bearer Token"}, wantStatus: http.StatusUnauthorized},
		{name: "combined credentials", headers: []string{"Bearer Token, Bearer Token"}, wantStatus: http.StatusUnauthorized},
		{name: "extra token padding", headers: []string{"Bearer Token="}, wantStatus: http.StatusUnauthorized},
	}
	// Exercise each header configuration without opening a socket.
	for _, tt := range tests {
		// Check the middleware response for this case.
		t.Run(tt.name, func(t *testing.T) {
			// Create the request and response recorder.
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			// Count downstream calls to detect forwarding even if a rejection has already been written.
			calls := 0
			// Build middleware that records downstream calls for this request.
			handler := lpsrv.RequireBearerAuth("Token", http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				// Record every downstream invocation.
				calls++
				// Check that middleware forwards the original request.
				if got != r {
					// Report an unexpected replacement of the request.
					t.Error("downstream handler did not receive the original request")
				}
				// Mark the request as authenticated.
				w.WriteHeader(http.StatusNoContent)
			}))
			// Set a header only when the case supplies values.
			if len(tt.headers) > 0 {
				// Preserve separate values to exercise duplicate detection.
				r.Header["Authorization"] = tt.headers
			}
			// Capture the response without a listener.
			w := httptest.NewRecorder()
			// Run the request through the middleware.
			handler.ServeHTTP(w, r)
			// Check whether the request was forwarded or rejected as expected.
			resp := recordedHandlerResponse(t, w, tt.wantStatus)
			// Check that rejected requests carry a Bearer challenge.
			if tt.wantStatus == http.StatusUnauthorized {
				// Compare the committed challenge with the expected scheme.
				checkHandlerString(t, "WWW-Authenticate", resp.Header.Get("WWW-Authenticate"), "Bearer")
				// Check that rejection did not invoke the downstream handler.
				if calls != 0 {
					// Report unexpected forwarding of a rejected request.
					t.Errorf("downstream calls = %d, want 0", calls)
				}
				// Skip the success assertions for rejected requests.
				return
			}
			// Check that an accepted request was forwarded exactly once.
			if calls != 1 {
				// Report an unexpected call count.
				t.Errorf("downstream calls = %d, want 1", calls)
			}
		})
	}
}
