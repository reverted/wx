package wx

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Logger interface {
	Fatal(a ...interface{})
	Fatalf(format string, a ...interface{})
	Error(a ...interface{})
	Errorf(format string, a ...interface{})
	Warn(a ...interface{})
	Warnf(format string, a ...interface{})
	Info(a ...interface{})
	Infof(format string, a ...interface{})
	Debug(a ...interface{})
	Debugf(format string, a ...interface{})
}

func NewWebServerFromEnv(logger Logger, handler http.Handler) http.Handler {

	target, err := url.Parse(os.Getenv("REVERTED_WX_API_URL"))
	if err != nil {
		logger.Fatal(err)
	}

	authServer := NewAuthServer(
		logger,
		FromEnv(),
	)

	proxyServer := NewProxyServer(
		logger,
		http.DefaultClient,
		target,
		authServer.ModifyHeader,
	)

	targetPath := strings.TrimRight(target.Path, "/") + "/"

	server := http.NewServeMux()
	server.HandleFunc("/auth/login", authServer.Login)
	server.HandleFunc("/auth/logout", authServer.Logout)
	server.HandleFunc("/auth/callback", authServer.Callback)
	server.HandleFunc("/auth/userinfo", authServer.UserInfo)
	server.HandleFunc(targetPath, proxyServer.Serve)
	server.Handle("/", handler)
	return server
}
