package wx

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

type Logger interface {
	Error(a ...interface{})
	Info(a ...interface{})
	Debug(a ...interface{})
}

func NewWebServer(
	logger Logger,
	target *url.URL,
	config oauth2.Config,
	handler http.Handler,
) http.Handler {

	authServer := NewAuthServer(
		logger,
		WithOAuthConfig(config),
	)

	proxyServer := NewProxyServer(
		logger,
		WithTarget(target),
		WithModifier(authServer.ModifyHeader),
	)

	proxyPath := strings.TrimRight(target.Path, "/") + "/"

	return New(authServer, proxyServer, proxyPath, handler)
}

func New(
	authServer *authServer,
	proxyServer *proxyServer,
	proxyPath string,
	handler http.Handler,
) http.Handler {

	server := http.NewServeMux()
	server.HandleFunc("/auth/login", authServer.Login)
	server.HandleFunc("/auth/logout", authServer.Logout)
	server.HandleFunc("/auth/callback", authServer.Callback)
	server.HandleFunc("/auth/userinfo", authServer.UserInfo)
	server.HandleFunc(proxyPath, proxyServer.Serve)
	server.Handle("/", handler)
	return server
}
