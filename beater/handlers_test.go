package beater

import (
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"fmt"

	"sync"
	"time"

	"strconv"

	"github.com/stretchr/testify/assert"
)

func TestIncCounter(t *testing.T) {
	req, err := http.NewRequest("POST", "_", nil)
	assert.Nil(t, err)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	for i := 1; i <= 5; i++ {
		for _, res := range []serverResponse{acceptedResponse, okResponse, forbiddenResponse(errors.New("")), unauthorizedResponse,
			requestTooLargeResponse, rateLimitedResponse, methodNotAllowedResponse, tooManyConcurrentRequestsResponse,
			cannotValidateResponse(errors.New("")), cannotDecodeResponse(errors.New("")),
			fullQueueResponse(errors.New("")), serverShuttingDownResponse(errors.New(""))} {
			sendStatus(w, req, res)
			assert.Equal(t, int64(i), res.counter.Get())
		}
	}
	assert.Equal(t, int64(60), responseCounter.Get())
	assert.Equal(t, int64(50), responseErrors.Get())
}

type noopHandler struct {
	wg *sync.WaitGroup
}

func (h noopHandler) ServeHTTP(_ http.ResponseWriter, _ *http.Request) {
	time.Sleep(time.Millisecond * 100)
	h.wg.Done()
}

func TestConcurrency(t *testing.T) {
	config := Config{ConcurrentRequests: 2, MaxRequestQueueTime: time.Second}
	var wg sync.WaitGroup
	h := concurrencyLimitHandler(&config, noopHandler{&wg})
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go h.ServeHTTP(nil, nil)
	}
	wg.Wait()
	// 3rd request must wait at least for one to complete
	assert.True(t, concurrentWait.Get() > 20, strconv.FormatInt(concurrentWait.Get(), 10))
}

func TestAccept(t *testing.T) {
	for idx, test := range []struct{ accept, expectedError, expectedContentType string }{
		{"application/json", "{\"error\":\"data validation error: error message\"}", "application/json"},
		{"*/*", "{\"error\":\"data validation error: error message\"}", "application/json"},
		{"text/html", "data validation error: error message", "text/plain; charset=utf-8"},
		{"", "data validation error: error message", "text/plain; charset=utf-8"},
	} {
		req, err := http.NewRequest("POST", "_", nil)
		assert.Nil(t, err)
		if test.accept != "" {
			req.Header.Set("Accept", test.accept)
		} else {
			delete(req.Header, "Accept")
		}
		w := httptest.NewRecorder()
		sendStatus(w, req, cannotValidateResponse(errors.New("error message")))
		resp := w.Result()
		body, _ := ioutil.ReadAll(resp.Body)
		assert.Equal(t, 400, w.Code)
		assert.Equal(t, test.expectedError, string(body), fmt.Sprintf("at index %d", idx))
		assert.Equal(t, test.expectedContentType, resp.Header.Get("Content-Type"), fmt.Sprintf("at index %d", idx))
	}
}

func TestIsAuthorized(t *testing.T) {
	reqAuth := func(auth string) *http.Request {
		req, err := http.NewRequest("POST", "_", nil)
		assert.Nil(t, err)
		req.Header.Add("Authorization", auth)
		return req
	}

	reqNoAuth, err := http.NewRequest("POST", "_", nil)
	assert.Nil(t, err)

	// Successes
	assert.True(t, isAuthorized(reqNoAuth, ""))
	assert.True(t, isAuthorized(reqAuth("foo"), ""))
	assert.True(t, isAuthorized(reqAuth("Bearer foo"), "foo"))

	// Failures
	assert.False(t, isAuthorized(reqNoAuth, "foo"))
	assert.False(t, isAuthorized(reqAuth("Bearer bar"), "foo"))
	assert.False(t, isAuthorized(reqAuth("Bearer foo extra"), "foo"))
	assert.False(t, isAuthorized(reqAuth("foo"), "foo"))
}
