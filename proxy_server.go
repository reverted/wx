package wx

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Modifier func(r *http.Request) error

type proxyOpt func(*proxyServer)

func WithClient(client *http.Client) proxyOpt {
	return func(self *proxyServer) {
		self.Client = client
	}
}

func WithTarget(target *url.URL) proxyOpt {
	return func(self *proxyServer) {
		self.Target = target
	}
}

func WithModifier(modifier Modifier) proxyOpt {
	return func(self *proxyServer) {
		self.Modifiers = append(self.Modifiers, modifier)
	}
}

func NewProxyServer(logger Logger, opts ...proxyOpt) *proxyServer {
	server := &proxyServer{
		Logger:    logger,
		Client:    http.DefaultClient,
		Modifiers: []Modifier{},
	}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

type proxyServer struct {
	Logger
	*http.Client
	Target    *url.URL
	Modifiers []Modifier
}

func (self *proxyServer) Serve(w http.ResponseWriter, r *http.Request) {

	req, err := self.NewRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		self.Logger.Errorf("new request : %v", err)
		return
	}

	resp, err := self.Client.Do(req)
	if err != nil {
		switch t := err.(type) {
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		case *statusError:
			http.Error(w, t.Error(), t.StatusCode)
		}
		self.Logger.Errorf("client do : %v", err)
		return
	}

	defer resp.Body.Close()

	for h, val := range resp.Header {
		for _, v := range val {
			w.Header().Add(h, v)
		}
	}

	w.WriteHeader(resp.StatusCode)

	if resp.Header.Get("Content-Type") == "text/event-stream" {
		self.Stream(w, req, resp)
		self.Logger.Info("streaming done")
	} else {
		io.Copy(w, resp.Body)
	}
}

func (self *proxyServer) NewRequest(r *http.Request) (*http.Request, error) {

	url := self.Target.ResolveReference(r.URL)

	self.Logger.Info("<<< ", r.URL.String())

	req, err := http.NewRequestWithContext(r.Context(), r.Method, url.String(), r.Body)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	for h, val := range r.Header {
		for _, v := range val {
			req.Header.Add(h, v)
		}
	}

	for _, modifier := range self.Modifiers {
		if err := modifier(req); err != nil {
			return nil, fmt.Errorf("modifier: %w", err)
		}
	}

	self.Logger.Info(">>> ", req.URL.String())

	return req, nil
}

func (self *proxyServer) Stream(w http.ResponseWriter, r *http.Request, resp *http.Response) {

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := resp.Body.Read(buf)
			if err != nil {
				self.Logger.Errorf("read body: %v", err)
				break
			}

			if _, err := w.Write(buf[:n]); err != nil {
				self.Logger.Errorf("write body: %v", err)
				break
			}

			if ctx.Err() != nil {
				break
			}
		}
		self.Logger.Info("copy done")
	}()

	for {
		select {
		case <-time.After(100 * time.Millisecond):
			flusher.Flush()
		case <-ctx.Done():
			self.Logger.Info("context done")
			return
		}
	}
}

func NewStatusError(statusCode int, err error) *statusError {
	return &statusError{
		StatusCode: statusCode,
		Err:        err,
	}
}

type statusError struct {
	StatusCode int
	Err        error
}

func (r *statusError) Error() string {
	return fmt.Sprintf("status %d: err %v", r.StatusCode, r.Err)
}
