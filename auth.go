// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"crypto/sha256" // Import the crypto/sha256 package for fixed-length credential hashing.
	"crypto/subtle" // Import the crypto/subtle package for constant-time digest comparison.
	"net/http"      // Import the net/http package for HTTP middleware and status responses.
	"strings"       // Import the strings package for whitespace validation on tokens.

	"github.com/thorsphere/tserr" // Import the tserr package for structured error construction.
)

// The following constants define the HTTP headers and scheme values for RFC 6750 bearer authentication.
const (
	authorizationHeader   = "Authorization"    // The HTTP request header carrying client credentials.
	wwwAuthenticateHeader = "WWW-Authenticate" // The HTTP response challenge header sent on 401 responses.
	bearerScheme          = "Bearer"           // The authentication scheme advertised in WWW-Authenticate.
)

// ValidateBearerToken returns nil if tok satisfies the RFC 6750 bearer token
// grammar and an error otherwise. It does not trim or decode tok.
func ValidateBearerToken(tok string) error {
	// If tok is empty or whitespace-only, reject it before wiring handlers.
	if strings.TrimSpace(tok) == "" {
		// Return an empty-token error when no usable secret was supplied.
		return tserr.Empty("token")
	}
	// Remove optional trailing padding before validating the required token characters.
	body := strings.TrimRight(tok, "=")
	// Reject padding-only tokens and characters outside the RFC 6750 alphabet.
	if body == "" || strings.ContainsFunc(body, func(c rune) bool {
		// Accept only ASCII letters, digits, and the explicitly permitted punctuation.
		return !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+/", c))
	}) {
		// Describe the format error without including the configured secret.
		return tserr.InvalidFormat(&tserr.InvalidFormatArgs{F: "token", Detail: "must follow the RFC 6750 bearer token grammar"})
	}
	// Return nil to indicate a syntactically valid bearer token.
	return nil
}

// writeUnauthorized sets the RFC 6750 WWW-Authenticate challenge header on w
// and writes a 401 Unauthorized plain-text error response.
func writeUnauthorized(w http.ResponseWriter) {
	// Advertise the Bearer authentication scheme per RFC 6750.
	w.Header().Set(wwwAuthenticateHeader, bearerScheme)
	// Reject the unauthenticated request with 401 Unauthorized.
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
}

// RequireBearerAuth returns an http.Handler that forwards requests to next when
// the incoming Authorization header carries tok as a single Bearer credential,
// and responds with 401 Unauthorized when tok is invalid or the credential does
// not match. An authenticated request receives 500 Internal Server Error if next
// is a nil interface or a nil http.HandlerFunc.
func RequireBearerAuth(tok string, next http.Handler) http.Handler {
	// Validate the configured token once before handling requests.
	validTok := ValidateBearerToken(tok) == nil
	// Precompute the SHA-256 digest of the configured token.
	expectedHash := sha256.Sum256([]byte(tok))
	// Check for a HandlerFunc whose nil value would be hidden by the interface.
	handlerFunc, isHandlerFunc := next.(http.HandlerFunc)
	// Treat both forms of nil as a missing downstream handler.
	missingHandler := next == nil || (isHandlerFunc && handlerFunc == nil)
	// Return an http.HandlerFunc that enforces bearer authentication on each request.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject all requests if the middleware was configured with an invalid token.
		if !validTok {
			// Write the 401 challenge response for an invalid configured token.
			writeUnauthorized(w)
			// Stop processing the unauthenticated request.
			return
		}
		// Read all Authorization header values to detect missing or duplicate headers.
		vals := r.Header.Values(authorizationHeader)
		// Reject requests that omit Authorization or supply multiple Authorization headers.
		if len(vals) != 1 {
			// Write the 401 challenge response when a single credential is not present.
			writeUnauthorized(w)
			// Stop processing the unauthenticated request.
			return
		}
		// Split the scheme from the credential at the first ASCII space.
		scheme, credential, found := strings.Cut(vals[0], " ")
		// Reject missing separators and non-Bearer schemes.
		if !found || !strings.EqualFold(scheme, bearerScheme) {
			// Write the 401 challenge response for an invalid scheme or separator.
			writeUnauthorized(w)
			// Stop processing the unauthenticated request.
			return
		}
		// Consume any additional ASCII spaces allowed by the Bearer separator.
		credential = strings.TrimLeft(credential, " ")
		// Hash the supplied token to a fixed 32-byte digest.
		gotHash := sha256.Sum256([]byte(credential))
		// Compare the fixed-length digests in constant time to prevent content and length timing leaks.
		if subtle.ConstantTimeCompare(gotHash[:], expectedHash[:]) != 1 {
			// Write the 401 challenge response on credential mismatch.
			writeUnauthorized(w)
			// Stop processing the unauthenticated request.
			return
		}
		// Reject authenticated requests when the downstream handler is missing.
		if missingHandler {
			// Log and write a sanitized 500 Internal Server Error for the missing handler.
			writeInternalError(w, tserr.Empty("handler"))
			// Stop processing when no downstream handler is configured.
			return
		}
		// Forward the authenticated request to next.
		next.ServeHTTP(w, r)
	})
}
