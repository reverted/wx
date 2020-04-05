package wx

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type opt func(*authServer)

func FromEnv() opt {
	return func(self *authServer) {
		endpoint := oauth2.Endpoint{
			TokenURL: os.Getenv("REVERTED_WX_TOKEN_URL"),
			AuthURL:  os.Getenv("REVERTED_WX_AUTH_URL"),
		}

		self.Config = oauth2.Config{
			Endpoint:     endpoint,
			ClientID:     os.Getenv("REVERTED_WX_CLIENT_ID"),
			ClientSecret: os.Getenv("REVERTED_WX_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("REVERTED_WX_REDIRECT_URL"),
			Scopes:       strings.Split(os.Getenv("REVERTED_WX_SCOPE"), ","),
		}
	}
}

func WithOAuthConfig(config oauth2.Config) opt {
	return func(self *authServer) {
		self.Config = config
	}
}

func WithAuthCookieName(name string) opt {
	return func(self *authServer) {
		self.authCookieName = name
	}
}

func WithStateCookieName(name string) opt {
	return func(self *authServer) {
		self.stateCookieName = name
	}
}

func NewAuthServer(logger Logger, opts ...opt) *authServer {
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

func (self *authServer) Login(w http.ResponseWriter, r *http.Request) {

	state, err := self.encodeState(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     self.stateCookieName,
		Value:    state,
		Path:     "/",
		Expires:  time.Now().Add(time.Hour),
		HttpOnly: true,
	})

	url := self.Config.AuthCodeURL(state, oauth2.AccessTypeOffline)

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (self *authServer) Callback(w http.ResponseWriter, r *http.Request) {

	if err := self.checkError(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(err)
		return
	}

	state, err := self.decodeState(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(err)
		return
	}

	redirectUrl, err := url.Parse(state.RedirectUri)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(err)
		return
	}

	if redirectUrl.Host != "" {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(errors.New("Invalid redirect"))
		return
	}

	token, err := self.Config.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     self.authCookieName,
		Value:    token.TokenType + " " + token.AccessToken,
		Path:     "/",
		Expires:  token.Expiry,
		HttpOnly: true,
	})

	http.SetCookie(w, &http.Cookie{
		Name:   self.stateCookieName,
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, redirectUrl.String(), http.StatusTemporaryRedirect)
}

func (self *authServer) Logout(w http.ResponseWriter, r *http.Request) {

	redirectUri := r.FormValue("redirect_uri")
	if redirectUri == "" {
		redirectUri = "/"
	}

	redirectUrl, err := url.Parse(redirectUri)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(err)
		return
	}

	if redirectUrl.Host != "" {
		w.WriteHeader(http.StatusBadRequest)
		self.Logger.Error(errors.New("Invalid redirect"))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:   self.authCookieName,
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, redirectUrl.String(), http.StatusTemporaryRedirect)
}

func (self *authServer) UserInfo(w http.ResponseWriter, r *http.Request) {

	cookie, err := r.Cookie(self.authCookieName)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		self.Logger.Debug("Missing authorization cookie")
		return
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusUnauthorized)
		self.Logger.Debug("Marlformed authorization cookie")
		return
	}

	var claims map[string]interface{}
	if err = self.decode(parts[1], &claims); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		self.Logger.Error(err)
		return
	}

	json.NewEncoder(w).Encode(claims)
}

func (self *authServer) ModifyHeader(r *http.Request) error {

	cookie, err := r.Cookie(self.authCookieName)
	if err != nil {
		self.Logger.Debug("Missing authorization cookie")
		return nil
	}

	r.Header.Add("Authorization", cookie.Value)
	r.Header.Del("Cookie")
	return nil
}

func (self *authServer) encodeState(r *http.Request) (string, error) {

	redirectUri := r.FormValue("redirect_uri")
	if redirectUri == "" {
		redirectUri = "/"
	}

	state := State{
		RedirectUri: redirectUri,
		Timestamp:   time.Now().Unix(),
	}

	return self.encode(state)
}

func (self *authServer) decodeState(r *http.Request) (State, error) {

	var state State

	cookie, err := r.Cookie(self.stateCookieName)
	if err != nil {
		return state, err
	}

	if cookie.Value != r.FormValue("state") {
		return state, errors.New("Invalid state")
	}

	return state, self.decode(cookie.Value, &state)
}

func (self *authServer) encode(value interface{}) (string, error) {

	json, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	encoded := base64.StdEncoding.EncodeToString(json)

	return encoded, nil
}

func (self *authServer) decode(encoded string, value interface{}) error {

	if l := len(encoded) % 4; l > 0 {
		encoded += strings.Repeat("=", 4-l)
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}

	return json.Unmarshal(decoded, &value)
}

func (self *authServer) checkError(r *http.Request) error {

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
