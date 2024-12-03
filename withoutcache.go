//go:build !cache

package wx

import (
	"net/http"
	"time"
)

func NewWithCacheControl(logger Logger, ttl time.Duration, handler http.Handler) http.Handler {
	return handler
}

func NewAssetCache(fs http.FileSystem) *assetCache {
	return &assetCache{}
}

type assetCache struct{}

func (a *assetCache) Asset(asset string) (string, error) {
	return asset, nil
}
