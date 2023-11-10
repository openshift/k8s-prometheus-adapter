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

	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

// Without a limit, the adapter could flood the Prometheus API with many
// requests when there are many pods running in the cluster because a query for
// getting pod metrics across all namespaces translates into (2 x the number of
// namespaces) queries to the Prometheus API.
// In the worst case, the Prometheus pods can hit the maximum number of
// listening sockets allowed by the kernel (e.g. SOMAXCONN) leading to
// timed-out requests from other clients. In particular it can make the Kubelet
// liveness probes being reported as down and trigger Prometheus pod restarts.
// The number has been chosen from empirical data.
const maxConcurrentRequests = 100

var (
	inflightRequests = metrics.NewGauge(
		&metrics.GaugeOpts{
			Namespace: "prometheus_adapter",
			Subsystem: "prometheus_client",
			Name:      "inflight_requests",
			Help:      "Number of inflight requests to the Prometheus service",
		})

	maxRequests = metrics.NewGauge(
		&metrics.GaugeOpts{
			Namespace: "prometheus_adapter",
			Subsystem: "prometheus_client",
			Name:      "max_requests",
			Help:      "Maximum number of requests to the Prometheus service",
		})
)

func init() {
	legacyregistry.MustRegister(inflightRequests, maxRequests)
	maxRequests.Set(maxConcurrentRequests)
}

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
		inflightRequests.Inc()
		defer func() {
			inflightRequests.Dec()
			<-c.inflight
		}()
	case <-ctx.Done():
		return APIResponse{}, ctx.Err()
	}

	return c.c.Do(ctx, verb, endpoint, query)
}
