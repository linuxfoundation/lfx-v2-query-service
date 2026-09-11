// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pkgerrors "github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/require"
)

type blockedMappingClient struct {
	*MockOpenSearchClient
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
	fail    bool
}

func (c *blockedMappingClient) GetMapping(ctx context.Context, index string) (IndexMappings, error) {
	c.calls.Add(1)
	c.entered <- struct{}{}
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if c.fail {
		return nil, errors.New("mapping unavailable")
	}
	return IndexMappings{"a": {Properties: map[string]FieldMapping{"access_check_query": {Type: "keyword"}}}}, nil
}

func TestMappingReadSingleflight(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure and retry"
		}
		t.Run(name, func(t *testing.T) {
			client := &blockedMappingClient{MockOpenSearchClient: NewMockOpenSearchClient(), fail: fail}
			clock := time.Unix(100, 0)
			searcher := &OpenSearchSearcher{client: client, index: "alias", now: func() time.Time { return clock }}
			run := func() {
				client.entered = make(chan struct{}, 32)
				client.release = make(chan struct{})
				start := make(chan struct{})
				var wg sync.WaitGroup
				for i := 0; i < 32; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						field, err := searcher.resolveAccessKeyField(context.Background())
						if client.fail {
							var unavailable pkgerrors.ServiceUnavailable
							if !errors.As(err, &unavailable) {
								t.Errorf("got %v", err)
							}
						} else if err != nil || field != accessCheckQueryField {
							t.Errorf("field=%q err=%v", field, err)
						}
					}()
				}
				close(start)
				select {
				case <-client.entered:
				case <-time.After(10 * time.Second):
					t.Fatal("mapping read did not start")
				}
				close(client.release)
				wg.Wait()
			}
			run()
			require.Equal(t, int32(1), client.calls.Load())
			if fail {
				_, err := searcher.resolveAccessKeyField(context.Background())
				require.Error(t, err)
				require.Equal(t, int32(1), client.calls.Load(), "retry window has no new read")
				clock = clock.Add(accessKeyFieldRetryInterval)
				run()
				require.Equal(t, int32(2), client.calls.Load(), "one read at retry boundary")
				clock = clock.Add(accessKeyFieldRetryInterval)
				client.fail = false
				run()
				require.Equal(t, int32(3), client.calls.Load(), "one recovering read")
			}
			before := client.calls.Load()
			_, err := searcher.resolveAccessKeyField(context.Background())
			require.NoError(t, err)
			require.Equal(t, before, client.calls.Load(), "success remains cached within the interval")

			clock = clock.Add(5 * time.Minute)
			run()
			require.Equal(t, before+1, client.calls.Load(), "32 expiry callers share one revalidation")
			clock = clock.Add(5 * time.Minute)
			client.fail = true
			run()
			require.Equal(t, before+2, client.calls.Load(), "failed revalidation is also singleflight")
			clock = clock.Add(accessKeyFieldRetryInterval)
			client.fail = false
			run()
			require.Equal(t, before+3, client.calls.Load(), "recovery after expired success shares one read")
		})
	}
}
