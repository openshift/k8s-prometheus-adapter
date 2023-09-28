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
	"net/url"
)

const maxConcurrentRequests = 100

type requestLimitClient struct {
	c        GenericAPIClient
	inflight chan struct{}
}

func newRequestLimiter(c GenericAPIClient) GenericAPIClient {
	return &requestLimitClient{
		c:        c,
		inflight: make(chan struct{}, maxConcurrentRequests),
	}
}

func (c *requestLimitClient) Do(ctx context.Context, verb, endpoint string, query url.Values) (APIResponse, error) {
	select {
	case c.inflight <- struct{}{}:
		defer func() {
			<-c.inflight
		}()
	case <-ctx.Done():
		return APIResponse{}, ctx.Err()
	}

	return c.c.Do(ctx, verb, endpoint, query)
}
