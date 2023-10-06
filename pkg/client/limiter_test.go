/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestLimitClient(t *testing.T) {
	var (
		ctx     = context.Background()
		total   atomic.Int64
		unblock = make(chan struct{})
	)

	srvCtx, srvCancel := context.WithCancel(ctx)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total.Add(1)

		w.Write([]byte("{}"))
		if r.URL.Path == "/nonblocking" {
			return
		}

		// Requests will be blocked until the test closes the unblock channel or the test fails.
		select {
		case <-unblock:
		case <-srvCtx.Done():
		}
	}))
	defer func() {
		srvCancel()
		srv.Close()
	}()

	// Make as many requests as the max allowed number + 1.
	var (
		wg      sync.WaitGroup
		errChan = make(chan error, maxConcurrentRequests+1)
		u, _    = url.Parse(srv.URL)
		c       = NewGenericAPIClient(&http.Client{}, u, nil)
		do      = func(i int) {
			defer wg.Done()

			_, err := c.Do(ctx, "GET", "/", nil)
			if err != nil {
				err = fmt.Errorf("request #%d: %w", i, err)
			}
			errChan <- err
		}
	)
	for i := 0; i < maxConcurrentRequests; i++ {
		wg.Add(1)
		go func(i int) {
			do(i)
		}(i)
	}

	// Wait for the first maxConcurrentRequests requests to hit the server.
	for total.Load() != maxConcurrentRequests {
	}

	// Make one more request which should be blocked at the client level.
	wg.Add(1)
	go func() {
		do(maxConcurrentRequests)
	}()

	// Make one more request which should be canceled before hitting the server.
	ctx2, _ := context.WithTimeout(ctx, time.Second)
	_, err := c.Do(ctx2, "GET", "/nonblocking", nil)
	switch {
	case err == nil:
		t.Fatal("expected request to fail")
	case ctx2.Err() == nil:
		t.Fatal("expected request to timeout")
	}

	if total.Load() != maxConcurrentRequests {
		t.Fatalf("expected %d requests on the server side, got %d", maxConcurrentRequests, total.Load())
	}

	// Release all inflight requests.
	close(unblock)

	// Wait for all requests to complete.
	wg.Wait()

	// Check that no error was returned.
	close(errChan)
	for err := range errChan {
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}
}
