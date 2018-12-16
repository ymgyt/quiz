package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"

	"cloud.google.com/go/datastore"
	"github.com/PuerkitoBio/goquery"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/julienschmidt/httprouter"
	bf "github.com/russross/blackfriday"
	"github.com/sourcegraph/syntaxhighlight"
	"github.com/ymgyt/appkit/handlers"
	"github.com/ymgyt/appkit/services"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"
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
	DescriptionMD     string    `json:"description_md" datastore:",noindex"`
	DescriptionHTML   string    `json:"description_html" datastore:",noindex"`
	Options           []*Option `json:"options"`
	AnswerDescription string    `json:"answer_description" datastore:",noindex"`
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

	// markdownをhtmlに変換してsyntaxhighlightかける
	converter := &Markdown{}
	htm := converter.ConvertHTML([]byte(quiz.DescriptionMD))
	syntaxed, err := SyntaxHighlight(htm)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	quiz.DescriptionHTML = string(syntaxed)

	quiz, err = qh.PutToStorage(r.Context(), quiz)
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

// PickupInput -
type PickupInput struct {
	Max int
}

// PickupFromStorage -
// 全部とって、randomで返す. 複数取得とpickupする処理はわけたほうがよかった.
func (qh *QuizHandler) PickupFromStorage(ctx context.Context, input *PickupInput) ([]*Quiz, error) {
	q := datastore.NewQuery(quizKind)
	itr := qh.datastore.Run(ctx, q)

	var quizzes = make([]*Quiz, 0, input.Max) // たりないかもしれないので
	for {
		var quiz Quiz
		k, err := itr.Next(&quiz)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		quiz.ID = k.Encode()
		quizzes = append(quizzes, &quiz) // このaddressing 大丈夫?
	}

	return qh.pickup(quizzes, input.Max)
}

func (qh *QuizHandler) pickup(quizzes []*Quiz, n int) ([]*Quiz, error) {
	rand.Shuffle(len(quizzes), func(i, j int) {
		quizzes[i], quizzes[j] = quizzes[j], quizzes[i]
	})

	if len(quizzes) < n {
		n = len(quizzes)
	}
	return quizzes[:n], nil
}

// Markdown is markdown processor
type Markdown struct{}

// ConvertHTML -
func (m *Markdown) ConvertHTML(md []byte) []byte {
	return bf.Run(md)
}

// SyntaxHighlight replace code literal.
// https://zupzup.org/go-markdown-syntax-highlight/
func SyntaxHighlight(htm []byte) ([]byte, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htm))
	if err != nil {
		return nil, err
	}

	var parseErrs []error
	doc.Find("code[class*=\"language-\"]").Each(func(i int, s *goquery.Selection) {
		oldCode := s.Text()
		formatted, err := syntaxhighlight.AsHTML([]byte(oldCode))
		if err != nil {
			parseErrs = append(parseErrs, err)
			return
		}
		s.SetHtml(string(formatted))
	})

	// replace unnecessarily added html tags
	new, err := doc.Html()
	if err != nil {
		return nil, err
	}
	new = strings.Replace(new, "<html><head></head><body>", "", 1)
	new = strings.Replace(new, "</body></html>", "", 1)
	return []byte(new), nil
}
