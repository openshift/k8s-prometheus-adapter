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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total.Add(1)

		w.Write([]byte("{}"))
		if r.URL.Path == "/nonblocking" {
			return
		}

		// Requests will be blocked until the test closes the unblock channel.
		<-unblock
	}))
	defer srv.Close()

	// Make as many requests as the max allowed number + 1.
	var (
		wg      sync.WaitGroup
		errChan = make(chan error, maxConcurrentRequests+1)
		u, _    = url.Parse(srv.URL)
		c       = NewGenericAPIClient(&http.Client{}, u, nil)
	)
	for i := 0; i < maxConcurrentRequests+1; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			_, err := c.Do(ctx, "GET", "/", nil)
			if err != nil {
				err = fmt.Errorf("request #%d: %w", i, err)
			}
			errChan <- err
		}(i)
	}

	// Wait for the unblocked requests to hit the server.
	for total.Load() != maxConcurrentRequests {
	}

	// Make one more blocked request which should timeout before hitting the server.
	ctx2, _ := context.WithTimeout(ctx, time.Second)
	_, err := c.Do(ctx2, "GET", "/nonblocking", nil)
	switch {
	case err == nil:
		t.Fatalf("expected %dth request to fail", maxConcurrentRequests+2)
	case ctx2.Err() == nil:
		t.Fatalf("expected %dth request to timeout", maxConcurrentRequests+2)
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
