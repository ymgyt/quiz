package main

import (
	"net/http"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/ymgyt/appkit/services"
)

// User -
type User struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// AnonymouseUser -
var AnonymouseUser = &User{
	Name:      "noone",
	AvatarURL: "https://avatars2.githubusercontent.com/u/38001821?s=400&v=4", // 誰かのdefault icon
}

// UserFromMapClaims -
func UserFromMapClaims(c map[string]interface{}) *User {
	name, ok := c["login"].(string)
	if !ok || name == "" {
		return AnonymouseUser
	}
	return &User{
		Name:      name,
		AvatarURL: c["avatar_url"].(string),
	}
}

// UserFromReq -
func UserFromReq(r *http.Request) (u *User, found bool) {
	ctx := r.Context()
	idToken, ok := services.GetIDToken(ctx)
	if !ok {
		return nil, false
	}
	return UserFromMapClaims(idToken.Claims.(jwt.MapClaims)), true
}
