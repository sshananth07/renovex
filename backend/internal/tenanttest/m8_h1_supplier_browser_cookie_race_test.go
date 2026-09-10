package tenanttest_test

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// --- Test-infrastructure hardening: supplierBrowser cookie synchronization ---
//
// supplierBrowser's cookie jar (b.cookies) is shared, mutable state read and
// written by request(). TestM8PhaseH6_ConcurrentAcknowledgementsCreateOneReceipt
// already drives twenty concurrent goroutines through ONE shared browser
// (m8_h6_limits_test.go), which is a real concurrent-map-access hazard: it has
// stayed silent only because the acknowledge-outcome endpoint never sets a
// Set-Cookie header, so H6's burst happens to be read-only. Any endpoint that
// rotates a cookie under concurrent access would trip Go's "concurrent map
// read and map write" fatal error. This is not the H6 acknowledgement flake —
// it is a separate, latent test-infrastructure defect.

// rotatingCookieServer is a minimal HTTP handler that accepts concurrent
// requests, reads the caller's current "session" cookie, and returns a
// Set-Cookie with a freshly incremented value — the exact shape that would
// expose an unsynchronized cookie jar under concurrent supplierBrowser use.
type rotatingCookieServer struct {
	counter int64
}

func (s *rotatingCookieServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Reading the incoming cookie is part of the race surface under test: the
	// browser must hand this handler a coherent snapshot, not a jar being
	// mutated concurrently by another goroutine's response handling.
	_, _ = r.Cookie("session")

	next := atomic.AddInt64(&s.counter, 1)
	http.SetCookie(w, &http.Cookie{Name: "session", Value: strconv.FormatInt(next, 10)})
	w.WriteHeader(http.StatusOK)
}

// TestSupplierBrowserCookieRaceUnderConcurrentRequests drives many goroutines
// through ONE shared supplierBrowser, each issuing a request against a server
// that both reads and rotates the session cookie on every call — forcing
// overlapping reads and writes of b.cookies. A start barrier forces genuine
// overlap without relying on sleeps.
func TestSupplierBrowserCookieRaceUnderConcurrentRequests(t *testing.T) {
	server := &rotatingCookieServer{}
	browser := newSupplierBrowser(server)

	const concurrentRequests = 50
	start := make(chan struct{})
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(concurrentRequests)
	wait.Add(concurrentRequests)
	codes := make([]int, concurrentRequests)

	for index := range codes {
		go func(slot int) {
			defer wait.Done()
			ready.Done()
			<-start
			response := browser.doWithoutCSRF(t, http.MethodGet, "/rotate", nil)
			codes[slot] = response.Code
		}(index)
	}
	ready.Wait()
	close(start)
	wait.Wait()

	for index, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", index, code)
		}
	}

	finalValue, ok := browser.cookiesSnapshot()["session"]
	if !ok {
		t.Fatalf("browser retained no session cookie after concurrent burst")
	}
	if _, err := strconv.ParseInt(finalValue, 10, 64); err != nil {
		t.Fatalf("final session cookie value = %q, want a valid rotation counter: %v", finalValue, err)
	}
}

// TestSupplierBrowserCookieConcurrentSetCookieUpdatesDoNotRace isolates the
// write side: every goroutine's response carries a DIFFERENT Set-Cookie value,
// so the burst forces overlapping writes into b.cookies with no reads in the
// mix. -race catches an unsynchronized map write vs. map write exactly as
// readily as a read vs. write.
func TestSupplierBrowserCookieConcurrentSetCookieUpdatesDoNotRace(t *testing.T) {
	server := &rotatingCookieServer{}
	browser := newSupplierBrowser(server)

	const concurrentRequests = 50
	start := make(chan struct{})
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(concurrentRequests)
	wait.Add(concurrentRequests)

	for index := 0; index < concurrentRequests; index++ {
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			browser.doWithoutCSRF(t, http.MethodGet, "/rotate", nil)
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()

	if _, ok := browser.cookiesSnapshot()["session"]; !ok {
		t.Fatalf("browser retained no session cookie after concurrent Set-Cookie burst")
	}
}

// TestSupplierBrowserCookieReadWriteOverlapDoesNotPanic drives a mixed burst
// of readers (cookiesSnapshot) and writers (requests that rotate the cookie)
// concurrently. An unsynchronized Go map hit by a concurrent read and write
// does not merely race — it can crash the process with "fatal error:
// concurrent map read and map write", which -race would report but which is
// also worth asserting the test itself survives to completion.
func TestSupplierBrowserCookieReadWriteOverlapDoesNotPanic(t *testing.T) {
	server := &rotatingCookieServer{}
	browser := newSupplierBrowser(server)
	// Seed a cookie so early readers have something to observe.
	browser.doWithoutCSRF(t, http.MethodGet, "/rotate", nil)

	const writers = 30
	const readers = 30
	start := make(chan struct{})
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(writers + readers)
	wait.Add(writers + readers)

	for i := 0; i < writers; i++ {
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			browser.doWithoutCSRF(t, http.MethodGet, "/rotate", nil)
		}()
	}
	for i := 0; i < readers; i++ {
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			_ = browser.cookiesSnapshot()
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()

	// Reaching this line at all — without a fatal runtime crash — is the
	// assertion. A final coherent read confirms the jar is still usable.
	if _, ok := browser.cookiesSnapshot()["session"]; !ok {
		t.Fatalf("browser retained no session cookie after mixed read/write burst")
	}
}

// TestSupplierBrowserCookieResponseReplacesPreviousValue is a plain,
// single-goroutine functional check: a later Set-Cookie must overwrite the
// earlier value, exactly as before synchronization was added.
func TestSupplierBrowserCookieResponseReplacesPreviousValue(t *testing.T) {
	server := &rotatingCookieServer{}
	browser := newSupplierBrowser(server)

	browser.doWithoutCSRF(t, http.MethodGet, "/rotate", nil)
	first := browser.cookiesSnapshot()["session"]

	browser.doWithoutCSRF(t, http.MethodGet, "/rotate", nil)
	second := browser.cookiesSnapshot()["session"]

	if first == "" || second == "" {
		t.Fatalf("expected two non-empty session values, got %q then %q", first, second)
	}
	if first == second {
		t.Fatalf("second response did not replace the cookie value: still %q", second)
	}
}

// expiringCookieServer returns a Set-Cookie with MaxAge < 0 on demand,
// exercising supplierBrowser's existing deletion semantics under the new lock.
type expiringCookieServer struct{}

func (expiringCookieServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/set" {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "issued"})
	} else {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "", MaxAge: -1})
	}
	w.WriteHeader(http.StatusOK)
}

// TestSupplierBrowserCookieExpiryDeletesEntry confirms the pre-existing
// MaxAge < 0 deletion behavior in request() is unchanged now that it runs
// under the write lock.
func TestSupplierBrowserCookieExpiryDeletesEntry(t *testing.T) {
	browser := newSupplierBrowser(expiringCookieServer{})

	browser.doWithoutCSRF(t, http.MethodGet, "/set", nil)
	if _, ok := browser.cookiesSnapshot()["session"]; !ok {
		t.Fatalf("expected session cookie to be set before expiry")
	}

	browser.doWithoutCSRF(t, http.MethodGet, "/expire", nil)
	if _, ok := browser.cookiesSnapshot()["session"]; ok {
		t.Fatalf("expected session cookie to be deleted after MaxAge < 0 response")
	}
}

// TestSupplierBrowserCookieInstancesDoNotShareState confirms two independent
// supplierBrowser values (each with its own mutex and map) never observe each
// other's cookies — the mutex is per-instance, not global.
func TestSupplierBrowserCookieInstancesDoNotShareState(t *testing.T) {
	server := &rotatingCookieServer{}
	first := newSupplierBrowser(server)
	second := newSupplierBrowser(server)

	first.doWithoutCSRF(t, http.MethodGet, "/rotate", nil)

	if _, ok := second.cookiesSnapshot()["session"]; ok {
		t.Fatalf("second browser observed the first browser's cookie")
	}
	if _, ok := first.cookiesSnapshot()["session"]; !ok {
		t.Fatalf("first browser lost its own cookie")
	}
}

// blockingCookieServer holds a request open until told to proceed, letting a
// test deterministically observe whether a second goroutine can complete its
// snapshot phase WHILE the first request's round-trip is still in flight —
// which is only possible if the mutex is not held across the HTTP call.
type blockingCookieServer struct {
	release chan struct{}
	entered chan struct{}
}

func (s *blockingCookieServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	close(s.entered)
	<-s.release
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "after-block"})
	w.WriteHeader(http.StatusOK)
}

// TestSupplierBrowserCookieSnapshotDoesNotBlockOnInFlightRequest deliberately
// holds one request open inside the handler, then proves — deterministically,
// with no sleeps — that a second goroutine can still complete a
// cookiesSnapshot() call while the first request has not yet returned. If the
// browser held its mutex across the network round-trip, the second goroutine
// would deadlock waiting for RLock and this test would hang until -timeout.
func TestSupplierBrowserCookieSnapshotDoesNotBlockOnInFlightRequest(t *testing.T) {
	server := &blockingCookieServer{
		release: make(chan struct{}),
		entered: make(chan struct{}),
	}
	browser := newSupplierBrowser(server)

	requestDone := make(chan *http.Response)
	go func() {
		response := browser.doWithoutCSRF(t, http.MethodGet, "/hold", nil)
		requestDone <- response.Result()
	}()

	// Wait until the in-flight request is actually inside the handler (past
	// the snapshot phase, mutex released, blocked only on server.release).
	<-server.entered

	// While that request is still open, a second goroutine's snapshot must be
	// able to proceed immediately. A buffered channel plus a short select
	// against the still-open request lets us assert "did not block" without
	// timing assumptions: if snapshotDone fires before requestDone, the
	// mutex was not held across the round-trip.
	snapshotDone := make(chan struct{})
	go func() {
		_ = browser.cookiesSnapshot()
		close(snapshotDone)
	}()

	select {
	case <-snapshotDone:
		// Snapshot completed while the first request is still open — proves
		// the mutex was released before the network call.
	case <-requestDone:
		t.Fatalf("cookiesSnapshot did not complete until after the in-flight " +
			"request returned, indicating the mutex was held across the HTTP round-trip")
	}

	close(server.release)
	<-requestDone
}
