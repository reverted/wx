package wx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/golang/groupcache"
)

type contextKey string

const (
	contextKeyUrl     contextKey = "url"
	contextKeyHeaders contextKey = "headers"
)

func NewProxyCache(logger Logger, ttl time.Duration, getter groupcache.Getter) *proxyCache {
	return &proxyCache{
		Logger:   logger,
		Duration: ttl,
		Getter:   getter,
	}
}

type proxyCache struct {
	Logger
	groupcache.Getter
	time.Duration
}

func (c *proxyCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	url := r.URL.String()

	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKeyUrl, url)
	ctx = context.WithValue(ctx, contextKeyHeaders, r.Header)

	key := fmt.Sprintf("[%v]%v", time.Now().Round(c.Duration), url)

	c.Logger.Infof("fetching key : %v", key)

	var data []byte
	if err := c.Getter.Get(ctx, key, groupcache.AllocatingByteSliceSink(&data)); err != nil {
		c.serveError(w, err)
		return
	}

	c.Logger.Infof("found key : %v, size : %d bytes", key, len(data))

	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (c *proxyCache) serveError(w http.ResponseWriter, err error) {
	c.Logger.Errorf("error : %v", err)

	var httpError *HttpError
	if errors.As(err, &httpError) {
		http.Error(w, err.Error(), httpError.Status())
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func NewGroupCache(handler http.Handler) groupcache.Getter {
	getter := NewCacheGetter(handler)
	return groupcache.NewGroup("cache", 64<<20, getter) //64MB
}

type cacheWriter struct {
	groupcache.Sink

	header     http.Header
	bytes      *bytes.Buffer
	statusCode int
}

func NewCacheGetter(handler http.Handler) *cacheGetter {
	return &cacheGetter{
		Handler: handler,
	}
}

type cacheGetter struct {
	http.Handler
}

func (c *cacheGetter) Get(ctx context.Context, key string, dest groupcache.Sink) error {

	req, err := c.createRequest(ctx)
	if err != nil {
		return fmt.Errorf("create request [%v] : %w", key, err)
	}

	writer := NewCacheWriter(dest)
	c.Handler.ServeHTTP(writer, req)

	if err = writer.WriteCache(); err != nil {
		return fmt.Errorf("write cache [%v] : %w", key, err)
	}

	return nil
}

func (c *cacheGetter) createRequest(ctx context.Context) (*http.Request, error) {

	url, ok := ctx.Value(contextKeyUrl).(string)
	if !ok {
		return nil, fmt.Errorf("url missing from context")
	}

	headers, ok := ctx.Value(contextKeyHeaders).(http.Header)
	if !ok {
		return nil, fmt.Errorf("headers missing from context")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("request [%v] : %w", url, err)
	}

	req.Header = headers
	return req, nil
}

func NewCacheWriter(sink groupcache.Sink) *cacheWriter {
	return &cacheWriter{
		Sink:   sink,
		header: http.Header{},
		bytes:  bytes.NewBuffer([]byte{}),
	}
}

func (c *cacheWriter) Header() http.Header {
	return c.header
}

func (c *cacheWriter) Write(bytes []byte) (int, error) {
	return c.bytes.Write(bytes)
}

func (c *cacheWriter) WriteHeader(statusCode int) {
	c.statusCode = statusCode
}

func (c *cacheWriter) WriteCache() error {
	if c.statusCode >= 400 {
		return NewHttpError(c.statusCode)
	} else {
		return c.Sink.SetBytes(c.bytes.Bytes())
	}
}

func NewHttpError(statusCode int) *HttpError {
	return &HttpError{statusCode}
}

type HttpError struct {
	statusCode int
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("http error : %v", e.statusCode)
}

func (e *HttpError) Status() int {
	return e.statusCode
}
