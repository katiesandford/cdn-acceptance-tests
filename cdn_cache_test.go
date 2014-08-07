package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Should cache first response for an unspecified period of time if when it
// doesn't specify it's own cache headers. Subsequent requests should return
// a cached response.
func TestCacheFirstResponse(t *testing.T) {
	ResetBackends(backendsByPriority)

	testRequestsCachedIndefinite(t, nil)
}

// Should cache responses for the period defined in a `Expires: n` response
// header.
func TestCacheExpires(t *testing.T) {
	ResetBackends(backendsByPriority)

	const cacheDuration = time.Duration(5 * time.Second)

	handler := func(w http.ResponseWriter) {
		headerValue := time.Now().UTC().Add(cacheDuration).Format(http.TimeFormat)
		w.Header().Set("Expires", headerValue)
	}

	testRequestsCachedDuration(t, handler, cacheDuration)
}

// Should cache responses for the period defined in a `Cache-Control:
// max-age=n` response header.
func TestCacheCacheControlMaxAge(t *testing.T) {
	ResetBackends(backendsByPriority)

	const cacheDuration = time.Duration(5 * time.Second)
	headerValue := fmt.Sprintf("max-age=%.0f", cacheDuration.Seconds())

	handler := func(w http.ResponseWriter) {
		w.Header().Set("Cache-Control", headerValue)
	}

	testRequestsCachedDuration(t, handler, cacheDuration)
}

// Should cache responses for the period defined in a `Cache-Control:
// max-age=n` response header when a `Expires: n*2` header is also present.
func TestCacheExpiresAndMaxAge(t *testing.T) {
	ResetBackends(backendsByPriority)

	const cacheDuration = time.Duration(5 * time.Second)
	const expiresDuration = cacheDuration * 2

	maxAgeValue := fmt.Sprintf("max-age=%.0f", cacheDuration.Seconds())

	handler := func(w http.ResponseWriter) {
		expiresValue := time.Now().UTC().Add(expiresDuration).Format(http.TimeFormat)

		w.Header().Set("Expires", expiresValue)
		w.Header().Set("Cache-Control", maxAgeValue)
	}

	testRequestsCachedDuration(t, handler, cacheDuration)
}

// Should cache responses with a status code of 404. It's a common
// misconception that 404 responses shouldn't be cached; they should because
// they can be expensive to generate.
func TestCache404Response(t *testing.T) {
	ResetBackends(backendsByPriority)

	handler := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}

	testRequestsCachedIndefinite(t, handler)
}

// Should cache multiple distinct responses for the same URL when origin responds
// with a `Vary` header and clients provide requests with different values
// for that header.
func TestCacheVary(t *testing.T) {
	ResetBackends(backendsByPriority)

	const reqHeaderName = "CustomThing"
	const respHeaderName = "Reflected-" + reqHeaderName
	headerVals := []string{
		"first distinct",
		"second distinct",
		"third distinct",
	}

	req := NewUniqueEdgeGET(t)

	for _, populateCache := range []bool{true, false} {
		for _, headerVal := range headerVals {
			if populateCache {
				originServer.SwitchHandler(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Vary", reqHeaderName)
					w.Header().Set(respHeaderName, r.Header.Get(reqHeaderName))
				})
			} else {
				originServer.SwitchHandler(func(w http.ResponseWriter, r *http.Request) {
					t.Error("Request should not have made it to origin")
					w.Header().Set(respHeaderName, "not cached")
				})
			}

			req.Header.Set(reqHeaderName, headerVal)
			resp := RoundTripCheckError(t, req)
			defer resp.Body.Close()

			if recVal := resp.Header.Get(respHeaderName); recVal != headerVal {
				t.Errorf(
					"Request received wrong %q header. Expected %q, got %q",
					respHeaderName,
					headerVal,
					recVal,
				)
			}
		}
	}
}

// Should cache distinct responses for requests with the same path but
// different query params.
func TestCacheUniqueQueryParams(t *testing.T) {
	ResetBackends(backendsByPriority)

	const respHeaderName = "Request-RawQuery"

	req1 := NewUniqueEdgeGET(t)
	req2 := NewUniqueEdgeGET(t)

	if req1.URL.Path != req2.URL.Path {
		t.Fatalf(
			"Request paths do not match. Expected %q, got %q",
			req1.URL.Path,
			req2.URL.Path,
		)
	}
	if req1.URL.RawQuery == req2.URL.RawQuery {
		t.Fatalf(
			"Request query params do not differ. Expected %q != %q",
			req1.URL.RawQuery,
			req2.URL.RawQuery,
		)
	}

	for _, populateCache := range []bool{true, false} {
		for _, req := range []*http.Request{req1, req2} {
			if populateCache {
				originServer.SwitchHandler(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set(respHeaderName, r.URL.RawQuery)
				})
			} else {
				originServer.SwitchHandler(func(w http.ResponseWriter, r *http.Request) {
					t.Errorf(
						"Request with query param %q should not have made it to origin",
						r.URL.RawQuery,
					)
				})
			}

			resp := RoundTripCheckError(t, req)
			defer resp.Body.Close()

			if recVal := resp.Header.Get(respHeaderName); recVal != req.URL.RawQuery {
				t.Errorf(
					"Request received wrong %q header. Expected %q, got %q",
					respHeaderName,
					req.URL.RawQuery,
					recVal,
				)
			}
		}
	}
}

// Should cache distinct responses for requests with the same query params
// but paths of different case-sensitivity.
func TestCacheUniqueCaseSensitive(t *testing.T) {
	ResetBackends(backendsByPriority)

	const reqPath = "/CaseSensitive"
	const respHeaderName = "Request-Path"

	req1 := NewUniqueEdgeGET(t)
	req2 := NewUniqueEdgeGET(t)

	req1.URL.Path = strings.ToLower(reqPath)
	req2.URL.Path = strings.ToUpper(reqPath)
	req1.URL.RawQuery = req2.URL.RawQuery

	if req1.URL.Path == req2.URL.Path {
		t.Fatalf(
			"Request paths do not differ. Expected %q != %q",
			req1.URL.Path,
			req2.URL.Path,
		)
	}
	if req1.URL.RawQuery != req2.URL.RawQuery {
		t.Fatalf(
			"Request query params do not match. Expected %q, got %q",
			req1.URL.RawQuery,
			req2.URL.RawQuery,
		)
	}

	for _, populateCache := range []bool{true, false} {
		for _, req := range []*http.Request{req1, req2} {
			if populateCache {
				originServer.SwitchHandler(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set(respHeaderName, r.URL.Path)
				})
			} else {
				originServer.SwitchHandler(func(w http.ResponseWriter, r *http.Request) {
					t.Errorf(
						"Request with path %q should not have made it to origin",
						r.URL.Path,
					)
				})
			}

			resp := RoundTripCheckError(t, req)
			defer resp.Body.Close()

			if recVal := resp.Header.Get(respHeaderName); recVal != req.URL.Path {
				t.Errorf(
					"Request received wrong %q header. Expected %q, got %q",
					respHeaderName,
					req.URL.Path,
					recVal,
				)
			}
		}
	}
}
