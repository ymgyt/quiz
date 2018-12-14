package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/julienschmidt/httprouter"
	"github.com/ymgyt/appkit/handlers"
	"github.com/ymgyt/appkit/oauth2"
	"github.com/ymgyt/appkit/services"
	"go.uber.org/zap"
)

// Authorizer -
type Authorizer struct {
	logger       *zap.Logger
	ts           *handlers.TemplateSet
	oauth2Config *oauth2.Config
	jwtService   *services.JWT
}

// ServeHTTP is used to be middleware
func (a *Authorizer) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	token, found := services.GetIDToken(r.Context())
	if !found {
		// redirect to login page
		w.Header().Set("Location", "/login?next="+r.URL.Path)
		w.WriteHeader(http.StatusFound)
		return
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		panic("only map claims is supported now")
	}

	name, ok := mapClaims["login"].(string)
	if !ok || name == "" {
		w.Header().Set("Location", "/login?next="+r.URL.Path)
		w.WriteHeader(http.StatusFound)
		return
	}

	next(w, r)
}

// RenderLogin -
func (a *Authorizer) RenderLogin(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	next := r.URL.Query().Get("next")
	if next == "" {
		next = "/quiz/new"
	}

	err := a.ts.ExecuteTemplate(w, "login", struct {
		Next   string
		OAuth2 *oauth2.Config
	}{
		Next:   next,
		OAuth2: a.oauth2Config,
	})
	if err != nil {
		logger.Error("render login", zap.Error(err))
	}
}

// GithubCallback -
func (a *Authorizer) GithubCallback(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	log := a.logger.With(zap.String("sp", "github"))

	// check state
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if code == "" {
		panic("empty code")
	}
	log.Debug("oauth2", zap.String("code", code), zap.String("state", state))

	// fetch access token
	c := a.oauth2Config.Github
	tokenEndpoint := fmt.Sprintf("%s?client_id=%s&client_secret=%s&code=%s&state=%s", c.TokenURL, c.ClientID, c.ClientSecret, code, state)

	tokenReq, err := http.NewRequest(http.MethodPost, tokenEndpoint, nil)
	if err != nil {
		log.Error("oauth2", zap.Error(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	tokenReq.Header.Set("Accept", "application/json")

	tokenRes, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		log.Error("oauth2", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer tokenRes.Body.Close()

	var accessTokenPayload oauth2.AccessTokenResponse
	if err := json.NewDecoder(tokenRes.Body).Decode(&accessTokenPayload); err != nil {
		log.Error("oauth2", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	log.Debug("oauth2", zap.Reflect("token_response", accessTokenPayload))

	// この処理の中で、githubからuser情報の取得
	// jwtにencodeまで行う

	// githubからuser情報を取得
	res, err := a.fetchUser(accessTokenPayload.AccessToken)
	if err != nil {
		log.Error("fetch_github_user", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	log.Debug("fetch_github_user", zap.Reflect("user", res))

	// jwtにencode
	idToken, err := a.jwtService.Sign(jwt.MapClaims(res))
	if err != nil {
		log.Error("jwt_sign", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// stateにredirect先を仕込んでいる
	w.Header().Set("Location", fmt.Sprintf("%s?id_token=%s", state, idToken))
	w.WriteHeader(http.StatusFound)
}

func (a *Authorizer) fetchUser(accessToken string) (map[string]interface{}, error) {
	const ep = "https://api.github.com/user"

	req, err := http.NewRequest(http.MethodGet, ep, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "token "+accessToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var payload map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return a.truncateUnnecessaryFields(payload), nil
}

func (a *Authorizer) truncateUnnecessaryFields(src map[string]interface{}) map[string]interface{} {
	var dst = make(map[string]interface{})
	dst["login"] = src["login"]
	dst["avatar_url"] = src["avatar_url"]
	return dst
}
