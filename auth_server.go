package wx

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type authOpt func(*authServer)

func WithOAuthConfig(config oauth2.Config) authOpt {
	return func(a *authServer) {
		a.Config = config
	}
}

func WithAuthCookieName(name string) authOpt {
	return func(a *authServer) {
		a.authCookieName = name
	}
}

func WithStateCookieName(name string) authOpt {
	return func(a *authServer) {
		a.stateCookieName = name
	}
}

func NewAuthServer(logger Logger, opts ...authOpt) *authServer {
	server := &authServer{
		Logger:          logger,
		authCookieName:  "auth",
		stateCookieName: "state",
	}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

type authServer struct {
	Logger
	oauth2.Config
	authCookieName  string
	stateCookieName string
}

func (a *authServer) Login(w http.ResponseWriter, r *http.Request) {

	state, err := a.encodeState(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     a.stateCookieName,
		Value:    state,
		Path:     "/",
		Expires:  time.Now().Add(time.Hour),
		HttpOnly: true,
	})

	url := a.Config.AuthCodeURL(state, oauth2.AccessTypeOffline)

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (a *authServer) Callback(w http.ResponseWriter, r *http.Request) {

	if err := a.checkError(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(err)
		return
	}

	state, err := a.decodeState(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(err)
		return
	}

	redirectUrl, err := url.ParseRequestURI(state.RedirectUri)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(err)
		return
	}

	if redirectUrl.Host != "" {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(errors.New("invalid redirect"))
		return
	}

	token, err := a.Config.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     a.authCookieName,
		Value:    token.TokenType + " " + token.AccessToken,
		Path:     "/",
		Expires:  token.Expiry,
		HttpOnly: true,
	})

	http.SetCookie(w, &http.Cookie{
		Name:   a.stateCookieName,
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, redirectUrl.String(), http.StatusTemporaryRedirect)
}

func (a *authServer) Logout(w http.ResponseWriter, r *http.Request) {

	redirectUri := r.FormValue("redirect_uri")
	if redirectUri == "" {
		redirectUri = "/"
	}

	redirectUrl, err := url.ParseRequestURI(redirectUri)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(err)
		return
	}

	if redirectUrl.Host != "" {
		w.WriteHeader(http.StatusBadRequest)
		a.Logger.Error(errors.New("invalid redirect"))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:   a.authCookieName,
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, redirectUrl.String(), http.StatusTemporaryRedirect)
}

func (a *authServer) UserInfo(w http.ResponseWriter, r *http.Request) {

	cookie, err := r.Cookie(a.authCookieName)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		a.Logger.Debug("missing authorization cookie")
		return
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusUnauthorized)
		a.Logger.Debug("marlformed authorization cookie")
		return
	}

	var claims map[string]interface{}
	if err = a.decode(parts[1], &claims); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		a.Logger.Error(err)
		return
	}

	json.NewEncoder(w).Encode(claims)
}

func (a *authServer) ModifyHeader(r *http.Request) error {

	cookie, err := r.Cookie(a.authCookieName)
	if err != nil {
		a.Logger.Debug("missing authorization cookie")
		return nil
	}

	r.Header.Add("Authorization", cookie.Value)
	r.Header.Del("Cookie")
	return nil
}

func (a *authServer) encodeState(r *http.Request) (string, error) {

	redirectUri := r.FormValue("redirect_uri")
	if redirectUri == "" {
		redirectUri = "/"
	}

	state := State{
		RedirectUri: redirectUri,
		Timestamp:   time.Now().Unix(),
	}

	return a.encode(state)
}

func (a *authServer) decodeState(r *http.Request) (State, error) {

	var state State

	cookie, err := r.Cookie(a.stateCookieName)
	if err != nil {
		return state, err
	}

	if cookie.Value != r.FormValue("state") {
		return state, errors.New("invalid state")
	}

	return state, a.decode(cookie.Value, &state)
}

func (a *authServer) encode(value interface{}) (string, error) {

	json, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	encoded := base64.StdEncoding.EncodeToString(json)

	return encoded, nil
}

func (a *authServer) decode(encoded string, value interface{}) error {

	if l := len(encoded) % 4; l > 0 {
		encoded += strings.Repeat("=", 4-l)
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}

	return json.Unmarshal(decoded, &value)
}

func (a *authServer) checkError(r *http.Request) error {

	errType := r.FormValue("error")
	errDesc := r.FormValue("error_description")

	if errType != "" {
		return errors.New(errType + " : " + errDesc)
	}

	return nil
}

type State struct {
	RedirectUri string
	Timestamp   int64
}
