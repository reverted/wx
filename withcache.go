//go:build cache

package wx

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)

func NewWithCacheControl(logger Logger, ttl time.Duration, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer := NewCacheControlWriter(w, ttl)
		handler.ServeHTTP(writer, r)
	})
}

func NewCacheControlWriter(w http.ResponseWriter, ttl time.Duration) *cacheControlWriter {
	return &cacheControlWriter{
		ResponseWriter: w,
		Duration:       ttl,
	}
}

type cacheControlWriter struct {
	http.ResponseWriter
	time.Duration
}

func (self *cacheControlWriter) WriteHeader(statusCode int) {
	if statusCode == http.StatusOK {
		header := fmt.Sprintf("max-age=%v, private", self.Duration.Seconds())
		self.ResponseWriter.Header().Set("Cache-Control", header)
	}
	self.ResponseWriter.WriteHeader(statusCode)
}

func NewAssetCache(fs http.FileSystem) *assetCache {
	return &assetCache{
		FileSystem: fs,
		Cache:      map[string]string{},
	}
}

type assetCache struct {
	sync.Mutex
	http.FileSystem

	Cache map[string]string
}

func (self *assetCache) Asset(asset string) (string, error) {
	self.Lock()
	defer self.Unlock()

	id, found := self.Cache[asset]
	if !found {
		hash := md5.New()

		file, err := self.FileSystem.Open(asset)
		if err != nil {
			return "", fmt.Errorf("open [%s] : %w", asset, err)
		}

		defer file.Close()

		contents, err := ioutil.ReadAll(file)
		if err != nil {
			return "", fmt.Errorf("read [%s] : %w", asset, err)
		}

		_, err = hash.Write(contents)
		if err != nil {
			return "", fmt.Errorf("hash [%s] : %w", asset, err)
		}

		id = fmt.Sprintf("%x", hash.Sum(nil))
		self.Cache[asset] = id
	}

	return fmt.Sprintf("%s?id=%s", asset, id), nil
}
