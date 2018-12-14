package main

import (
	"context"
	"encoding/json"
	"net/http"

	"cloud.google.com/go/datastore"
	"github.com/davecgh/go-spew/spew"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/julienschmidt/httprouter"
	"github.com/ymgyt/appkit/handlers"
	"github.com/ymgyt/appkit/services"
	"go.uber.org/zap"
)

// QuizHandler -
type QuizHandler struct {
	ts        *handlers.TemplateSet
	datastore *datastore.Client
	logger    *zap.Logger
}

// RenderQuizForm -
func (qh *QuizHandler) RenderQuizForm(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	token, found := services.GetIDToken(r.Context())
	if !found {
		w.Header().Set("Location", "/login?next=/quiz/new")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	err := qh.ts.ExecuteTemplate(w, "quiz/form", struct {
		User *User
	}{
		User: UserFromMapClaims(token.Claims.(jwt.MapClaims)),
	})
	if err != nil {
		qh.logger.Error("render quiz form", zap.Error(err))
	}
}

// Quiz -
type Quiz struct {
	ID                string `json:"id"`
	User              *User
	DescriptionMD     string    `json:"description_md"`
	DescriptionHTML   string    `json:"description_html"`
	Options           []*Option `json:"options"`
	AnswerDescription string    `json:"answer_description"`
}

// Option -
type Option struct {
	Index       int    `json:"index"`
	Description string `json:"description"`
	IsAnswer    bool   `json:"is_answer"` // 注意が必要なfield
}

// api

// Create -
func (qh *QuizHandler) Create(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	quiz, err := qh.readQuiz(r)
	if err != nil {
		fail(w, http.StatusInternalServerError, &apiResponse{Err: err})
		return
	}

	user, found := UserFromReq(r)
	if !found {
		unauthorized(w)
		return
	}
	quiz.User = user

	// TODO どこかでhtmlへの変換いれる必要あり

	quiz, err = qh.PutToStorage(r.Context(), quiz)

	spew.Dump("save quiz", quiz)
	(&apiResponse{Data: quiz, Err: err}).write(w)
}

// Get -
func (qh *QuizHandler) Get(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	encodedID := params.ByName("id")
	quiz, err := qh.FetchFromStorage(r.Context(), encodedID)
	(&apiResponse{Data: quiz, Err: err}).write(w)
}

func (qh *QuizHandler) readQuiz(r *http.Request) (*Quiz, error) {
	defer r.Body.Close()
	var quiz Quiz
	return &quiz, json.NewDecoder(r.Body).Decode(&quiz)
}

type apiResponse struct {
	Data interface{} `json:"data"`
	Err  error       `json:"err"`
}

func (r *apiResponse) write(w http.ResponseWriter) {
	encoded, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	w.Write(encoded)
}

func fail(w http.ResponseWriter, code int, res *apiResponse) {
	encoded, err := json.Marshal(res)
	if err != nil {
		panic(err)
	}
	w.WriteHeader(code)
	w.Write(encoded)
}

func unauthorized(w http.ResponseWriter) {
	fail(w, http.StatusUnauthorized, &apiResponse{})
}

// storage operation

const (
	quizKind = "Quiz"
)

// PutToStorage -
func (qh *QuizHandler) PutToStorage(ctx context.Context, quiz *Quiz) (*Quiz, error) {
	var k *datastore.Key
	if quiz.ID == "" {
		k = datastore.IncompleteKey(quizKind, nil)
	} else {
		k = datastore.NameKey(quizKind, quiz.ID, nil)
	}
	nk, err := qh.datastore.Put(ctx, k, quiz)
	if err != nil {
		return nil, err
	}

	quiz.ID = nk.Encode()
	return quiz, nil
}

// FetchFromStorage -
func (qh *QuizHandler) FetchFromStorage(ctx context.Context, encodedID string) (*Quiz, error) {
	k, err := datastore.DecodeKey(encodedID)
	if err != nil {
		return nil, err
	}
	var quiz Quiz
	return &quiz, qh.datastore.Get(ctx, k, &quiz)
}
