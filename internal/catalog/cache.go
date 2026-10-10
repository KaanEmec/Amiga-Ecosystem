package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// Cache holds the rendered public catalogue JSON and its ETag. It builds
// lazily on the first Get after construction or Invalidate.
type Cache struct {
	build func(context.Context) ([]byte, error)

	mu   sync.Mutex
	body []byte
	etag string
}

// NewCache returns an empty cache that renders with build.
func NewCache(build func(context.Context) ([]byte, error)) *Cache {
	return &Cache{build: build}
}

// Get returns the cached body and its strong ETag (quoted sha256 hex),
// building it if needed. The returned slice must not be modified.
// The lock is held while building, so an Invalidate that follows a
// commit always waits for any build that may have read older data.
func (c *Cache) Get(ctx context.Context) ([]byte, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.body != nil {
		return c.body, c.etag, nil
	}
	body, err := c.build(ctx)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(body)
	c.body, c.etag = body, `"`+hex.EncodeToString(sum[:])+`"`
	return c.body, c.etag, nil
}

// Invalidate drops the cached body; the next Get rebuilds it.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.body, c.etag = nil, ""
	c.mu.Unlock()
}
