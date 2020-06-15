package wx

import (
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

	resp, err := self.Proxy(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		self.Logger.Error(err)
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
		self.Stream(w, r, resp)
	} else {
		io.Copy(w, resp.Body)
	}
}

func (self *proxyServer) Proxy(r *http.Request) (*http.Response, error) {

	url := self.Target.ResolveReference(r.URL)

	self.Logger.Info("<<< ", r.URL.String())
	defer self.Logger.Info(">>> ", url.String())

	req, err := http.NewRequest(r.Method, url.String(), r.Body)
	if err != nil {
		return nil, err
	}

	for h, val := range r.Header {
		for _, v := range val {
			req.Header.Add(h, v)
		}
	}

	for _, modifier := range self.Modifiers {
		if err := modifier(req); err != nil {
			return nil, err
		}
	}

	return self.Client.Do(req)
}

func (self *proxyServer) Stream(w http.ResponseWriter, r *http.Request, resp *http.Response) {

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusBadRequest)
		return
	}

	go func() {
		io.Copy(w, resp.Body)
	}()

	for {
		select {
		case <-time.After(100 * time.Millisecond):
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}
