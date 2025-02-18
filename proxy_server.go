package wx

import (
	"errors"
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
	return func(p *proxyServer) {
		p.Target = target
	}
}

func WithModifier(modifier Modifier) proxyOpt {
	return func(p *proxyServer) {
		p.Modifiers = append(p.Modifiers, modifier)
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

func (p *proxyServer) Serve(w http.ResponseWriter, r *http.Request) {

	req, err := p.NewRequest(r)
	if err != nil {
		p.handleError(w, fmt.Errorf("new request: %w", err))
		return
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		p.handleError(w, fmt.Errorf("client do: %w", err))
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
		p.Stream(w, req, resp)
		p.Logger.Info("streaming done")
	} else {
		io.Copy(w, resp.Body)
	}
}

func (p *proxyServer) handleError(w http.ResponseWriter, err error) {

	var statusErr *statusError
	if errors.As(err, &statusErr) {
		p.Logger.Errorf("handle status error: %v", err)
		http.Error(w, statusErr.Error(), statusErr.StatusCode)
	} else {

		p.Logger.Errorf("handle error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (p *proxyServer) NewRequest(r *http.Request) (*http.Request, error) {

	url := p.Target.ResolveReference(r.URL)

	p.Logger.Info("<<< ", r.URL.String())

	req, err := http.NewRequestWithContext(r.Context(), r.Method, url.String(), r.Body)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	for h, val := range r.Header {
		for _, v := range val {
			req.Header.Add(h, v)
		}
	}

	for _, modifier := range p.Modifiers {
		if err := modifier(req); err != nil {
			return nil, fmt.Errorf("modifier: %w", err)
		}
	}

	p.Logger.Info(">>> ", req.URL.String())

	return req, nil
}

func (p *proxyServer) Stream(w http.ResponseWriter, r *http.Request, resp *http.Response) {

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
				p.Logger.Errorf("read body: %v", err)
				break
			}

			if _, err := w.Write(buf[:n]); err != nil {
				p.Logger.Errorf("write body: %v", err)
				break
			}

			if ctx.Err() != nil {
				break
			}
		}
		p.Logger.Info("copy done")
	}()

	for {
		select {
		case <-time.After(100 * time.Millisecond):
			flusher.Flush()
		case <-ctx.Done():
			p.Logger.Info("context done")
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
