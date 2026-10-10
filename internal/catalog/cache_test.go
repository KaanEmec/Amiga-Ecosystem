package catalog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCache(t *testing.T) {
	t.Parallel()
	var builds atomic.Int32
	body := []byte(`{"v":1}`)
	c := NewCache(func(context.Context) ([]byte, error) {
		builds.Add(1)
		return body, nil
	})

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, _, err := c.Get(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := builds.Load(); n != 1 {
		t.Errorf("built %d times, want 1", n)
	}

	_, etag1, _ := c.Get(t.Context())
	if len(etag1) != 66 || etag1[0] != '"' {
		t.Errorf("etag = %s, want quoted sha256 hex", etag1)
	}
	body = []byte(`{"v":2}`)
	c.Invalidate()
	got, etag2, _ := c.Get(t.Context())
	if string(got) != `{"v":2}` || etag2 == etag1 || builds.Load() != 2 {
		t.Errorf("after Invalidate: body=%s etag changed=%v builds=%d", got, etag2 != etag1, builds.Load())
	}
}

func TestCache_BuildError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	c := NewCache(func(context.Context) ([]byte, error) { return nil, boom })
	if _, _, err := c.Get(t.Context()); !errors.Is(err, boom) {
		t.Errorf("Get err = %v, want boom", err)
	}
}
