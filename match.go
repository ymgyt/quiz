package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/ymgyt/appkit/handlers"
	"go.uber.org/zap"
)

// Init -
func (mg *MatchGroup) Init() {
	mg.m = make(map[string]*Match)

	match := newMatch(
		&MatchConfig{
			QuizNum: 2,
		},
		"100",
		mg.qh,
		mg.logger,
	)
	mg.m["100"] = match
	go mg.m["100"].run()
}

// MatchGroup -
type MatchGroup struct {
	ts       *handlers.TemplateSet
	upgrader websocket.Upgrader
	logger   *zap.Logger
	m        map[string]*Match
	qh       *QuizHandler
}

// RenderMatch -
func (mg *MatchGroup) RenderMatch(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := mg.ts.ExecuteTemplate(w, "match", nil); err != nil {
		mg.logger.Error("render", zap.Error(err))
	}
}

// ServeHTTP for websocket
func (mg *MatchGroup) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ps := strings.Split(r.URL.Path, "/")
	id := ps[len(ps)-1]

	// matchは事前に作成されている前提
	m, found := mg.m[id]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// 誰が接続してきたか確認
	user, found := UserFromReq(r)
	if !found {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	conn, err := mg.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("ws:upgrade fail", zap.Error(err))
		return
	}

	client := &Client{
		user:   user,
		conn:   conn,
		send:   make(chan []byte), // bufferにする..?
		match:  m,
		logger: mg.logger.With(zap.String("user", user.Name)),
	}
	client.match.register <- client
	go client.read()
	go client.write()
}

// StartMatch -
func (mg *MatchGroup) StartMatch(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	id := params.ByName("id")
	m, found := mg.m[id]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	m.Start()
	w.WriteHeader(http.StatusOK)
}

// NextQuiz -
func (mg *MatchGroup) NextQuiz(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	id := params.ByName("id")
	m, found := mg.m[id]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	m.nextQuiz()
	w.WriteHeader(http.StatusOK)
}

type submission struct {
	QuizIdx   int `json:"quiz_idx"`
	OptionIdx int `json:"option_idx"` // 選択肢1がidx 0に注意
}

// HandleSubmit is handler for process user quiz submission.
func (mg *MatchGroup) HandleSubmit(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	id := params.ByName("id")
	m, found := mg.m[id]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// 誰が接続してきたか確認
	user, found := UserFromReq(r)
	if !found {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	submission, err := mg.readSubmission(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.handleSubmission(user, submission)
	w.WriteHeader(http.StatusOK)
}

func newMatch(cfg *MatchConfig, name string, qh *QuizHandler, logger *zap.Logger) *Match {
	ctx := context.Background()
	quizzes, err := qh.PickupFromStorage(ctx, &PickupInput{Max: cfg.QuizNum})
	if err != nil {
		panic(err)
	}
	answerVisibilities := make([]bool, len(quizzes))

	return &Match{
		qh:                      qh,
		logger:                  logger.With(zap.String("name", name)),
		register:                make(chan *Client),
		unregister:              make(chan *Client),
		answer:                  make(chan []byte),
		clients:                 make(map[*Client]bool),
		contexts:                make(map[string]*Context),
		status:                  initializing,
		config:                  cfg,
		quizzes:                 quizzes,
		quizeAnswerVisibilities: answerVisibilities,
		currentQuiz:             -1, // nextQuiz呼んではじめられるように
	}
}

func (mg *MatchGroup) readSubmission(r *http.Request) (*submission, error) {
	defer r.Body.Close()
	var submission submission
	return &submission, json.NewDecoder(r.Body).Decode(&submission)
}

const (
	initializing = "initializing"
	starting     = "starting"
)

// QuizResult -
type QuizResult struct {
	OptionSubmitted       bool // userから回答の投稿があったかどうか
	QuizIdx               int
	OptionIdx             int
	Correct               bool
	UserCanGetTheirResult *bool // quizに正解したかどうかuserにわかるようにしてよいか
}

// Context -
type Context struct {
	Results []QuizResult
}

// MatchConfig -
type MatchConfig struct {
	QuizNum int
}

// Match -
type Match struct {
	name   string
	qh     *QuizHandler
	logger *zap.Logger

	// register requests from client
	register   chan *Client
	unregister chan *Client
	answer     chan []byte

	clients  map[*Client]bool
	contexts map[string]*Context // keyはuser.Name
	config   *MatchConfig

	status string
	// quiz関連
	quizzes                 []*Quiz
	quizeAnswerVisibilities []bool // 各quizの正解の可視性
	currentQuiz             int
}

// Start -
func (m *Match) Start() {
	m.status = starting
	m.logger.Info("match start")
	m.nextQuiz()
	m.updateState()
}

func (m *Match) run() {
	for {
		select {
		case client := <-m.register:
			m.registerClient(client)
		case client := <-m.unregister:
			if _, ok := m.clients[client]; ok {
				m.logger.Info("unregister", zap.String("user", client.user.Name))
				delete(m.clients, client)
				close(client.send)
			}
		case answer := <-m.answer:
			m.logger.Info("match", zap.String("answer", string(answer)))
			for client := range m.clients {
				select {
				case client.send <- answer:
				default:
					// ここはcloseしてしまってよい..?
					close(client.send)
					delete(m.clients, client)
				}
			}
		}
		m.updateState()
	}
}

func (m *Match) registerClient(client *Client) {
	m.logger.Info("register", zap.String("user", client.user.Name))
	m.clients[client] = true

	// contextの初期化処理
	user := client.user
	ctx, found := m.contexts[user.Name]
	if !found {
		ctx = &Context{Results: make([]QuizResult, len(m.quizzes))}
		m.contexts[user.Name] = ctx
	}
}

func (m *Match) nextQuiz() {
	// 現時点までに出題したquizの答えを発表
	for i := 0; i <= m.currentQuiz; i++ {
		m.quizeAnswerVisibilities[i] = true
	}
	m.currentQuiz++
	if m.currentQuiz >= len(m.quizzes) {
		m.currentQuiz = len(m.quizzes) - 1
	}
	m.updateState()
}

func (m *Match) handleSubmission(user *User, submission *submission) {
	m.logger.Info("submission", zap.String("user", user.Name), zap.Int("quiz", submission.QuizIdx), zap.Int("option", submission.OptionIdx))
	// 未選択状態でsubmitするとindexが-1なので握りつぶす
	if submission.OptionIdx == -1 {
		m.logger.Warn("submission", zap.Int("invalid option index", submission.OptionIdx))
		return
	}
	c, found := m.contexts[user.Name]
	if !found {
		m.logger.Warn("submission", zap.String("user not found", user.Name))
		return
	}

	if len(c.Results) <= submission.QuizIdx {
		m.logger.Warn("submission", zap.Int("invalid quiz idx", submission.QuizIdx))
		return
	}
	r := c.Results[submission.QuizIdx]
	r.OptionSubmitted = true
	r.QuizIdx = submission.QuizIdx
	r.OptionIdx = submission.OptionIdx
	r.Correct = m.isCorrect(submission.QuizIdx, submission.OptionIdx)
	r.UserCanGetTheirResult = &(m.quizeAnswerVisibilities[submission.QuizIdx])
	c.Results[submission.QuizIdx] = r

	m.updateState()
}

func (m *Match) isCorrect(quizIdx, optionIdx int) bool {
	q := m.quizzes[quizIdx]
	for _, opt := range q.Options {
		if optionIdx == opt.Index {
			return opt.IsAnswer
		}
	}
	return false
}

func (m *Match) updateState() {
	state := m.state()
	encoded := state.encode()
	for client := range m.clients {
		fmt.Println("send", client.user.Name)
		select {
		case client.send <- encoded:
		default:
			m.logger.Warn("send state fail", zap.String("client", client.user.Name))
			close(client.send)
			delete(m.clients, client)
		}
	}
}

func (m *Match) state() *State {
	var users []*User
	for c := range m.clients {
		users = append(users, c.user)
	}
	var quiz *Quiz
	if m.currentQuiz >= 0 && m.currentQuiz < len(m.quizzes) {
		quiz = m.quizzes[m.currentQuiz]
	}

	s := &State{
		Users:    users,
		Quiz:     quiz,
		QuizIdx:  m.currentQuiz,
		Contexts: m.contexts,

		Config: m.config,
	}
	// spew.Dump(s)
	s.match = m

	return s
}

const (
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

// Client represents client connection.
type Client struct {
	user   *User
	logger *zap.Logger

	match *Match
	conn  *websocket.Conn
	send  chan []byte
}

type msg struct {
	Code    int    `json:"code"`
	ID      int    `json:"id"`
	Message string `json:"message"`
}

func (c *Client) read() {
	defer func() {
		// c.logger.Debug(c.user.Name, zap.String("msg", "read defer"))
		c.match.unregister <- c
		c.conn.Close()
	}()
	// c.conn.SetReadLimit()
	c.conn.SetWriteDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("client", zap.Error(err))
			}
			break
		}
		c.logger.Debug(c.user.Name, zap.String("read_message", string(message)))

		var msg msg
		if err := json.Unmarshal(message, &msg); err != nil {
			panic(err)
		}
		spew.Dump(msg)

		c.match.answer <- message
	}
}

func (c *Client) write() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		c.logger.Debug(c.user.Name, zap.String("msg", "write defer"))
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// match closed channel
				c.logger.Info("client", zap.String("msg", "close"))
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				c.logger.Error("client", zap.Error(err))
				return
			}
			// c.logger.Debug(c.user.Name, zap.String("write_message", string(message)))
			w.Write(message)

			// sampleだとここでさらにc.sendからreadして書き込みをおこなっている

			if err := w.Close(); err != nil {
				c.logger.Error("client", zap.Error(err))
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Error("client", zap.Error(err))
				return
			}
		}
	}
}
