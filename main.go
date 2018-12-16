package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/gorilla/websocket"

	"cloud.google.com/go/datastore"
	"github.com/julienschmidt/httprouter"
	"github.com/urfave/negroni"
	"github.com/ymgyt/appkit/handlers"
	"github.com/ymgyt/appkit/logging"
	"github.com/ymgyt/appkit/middlewares"
	"github.com/ymgyt/appkit/oauth2"
	"github.com/ymgyt/appkit/server"
	"github.com/ymgyt/appkit/services"
	"go.uber.org/zap"
)

var (
	mode                        string
	root                        string
	host                        string
	port                        string
	gcpProjectID                string
	gcpServiceAccountCredential string
	githubClientID              string
	githubClientSecret          string

	logger     *zap.Logger
	hmacSecret = []byte("should_be_more_secret")
)

func router(ctx context.Context) http.Handler {
	r := httprouter.New()

	static := handlers.MustStatic(root+"/static", "/static")
	r.GET("/static/*filepath", static.ServeStatic)

	ts := handlers.MustTemplateSet(&handlers.TemplateSetConfig{
		Root:         root + "/templates",
		AlwaysReload: mode == "development",
	})

	// mw
	jwtMW := middlewares.MustJWTVerifier(&middlewares.JWTVerifyConfig{
		Logger:     logger,
		HMACSecret: hmacSecret,
	})
	authorizer := &Authorizer{
		ts:           ts,
		oauth2Config: newOAuth2Config(),
		logger:       logger,
		jwtService:   &services.JWT{HMACSecret: hmacSecret},
	}
	withAuthorize := func(h httprouter.Handle) http.Handler {
		n := negroni.New(jwtMW, authorizer)
		n.UseHandlerFunc(callwithParams(r, h))
		return n
	}

	r.GET("/login", authorizer.RenderLogin)
	r.GET("/oauth/github/callback", authorizer.GithubCallback)

	qh := &QuizHandler{logger: logger, ts: ts, datastore: datastoreClient(ctx)}
	r.Handler("GET", "/quiz/:id", withAuthorize(qh.RenderQuizForm))
	r.Handler("POST", "/api/v1/quiz/new", withAuthorize(qh.Create))
	r.Handler("GET", "/api/v1/quiz/:id", withAuthorize(qh.Get))

	mg := &MatchGroup{
		upgrader: websocket.Upgrader{
			ReadBufferSize: 1024, WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		logger: logger,
		ts:     ts,
		qh:     qh,
	}
	mg.Init() // 本当はapi callするところ
	r.Handler("GET", "/match/:id", withAuthorize(mg.RenderMatch))
	r.POST("/api/v1/match/:id/start", mg.StartMatch)
	r.Handler("POST", "/api/v1/match/:id/submission", withAuthorize(mg.HandleSubmit))

	// httprouterがhttp.Hijackerを実装していないので、websocketは直接うける
	go func() {
		n := negroni.New(jwtMW, authorizer)
		n.UseHandler(mg)
		if err := http.ListenAndServe(":9002", n); err != nil {
			logger.Error("ws", zap.Error(err))
		}
	}()

	common := negroni.New(middlewares.MustLogging(&middlewares.LoggingConfig{
		Logger:  logger,
		Console: true,
	}))
	common.UseHandler(r)

	return common
}

func main() {
	ctx := context.Background()

	logger = logging.Must(&logging.Config{
		Out:   os.Stdout,
		Level: "debug",
	})

	s := server.Must(&server.Config{
		Addr:            ":" + port,
		DisableHTTPS:    true,
		Handler:         router(ctx),
		DatastoreClient: datastoreClient(ctx),
	})

	fmt.Println("running on ", port)
	fmt.Println(s.Run())
}

func datastoreClient(ctx context.Context) *datastore.Client {
	b, err := ioutil.ReadFile(gcpServiceAccountCredential)
	if err != nil {
		panic(err)
	}
	c, err := services.NewDatastore(ctx, gcpProjectID, b)
	if err != nil {
		panic(err)
	}
	return c
}

func newOAuth2Config() *oauth2.Config {
	return &oauth2.Config{
		Github: &oauth2.Entry{
			Endpoint: &oauth2.Endpoint{
				AuthorizeURL: "https://github.com/login/oauth/authorize",
				TokenURL:     "https://github.com/login/oauth/access_token",
			},
			Credential: &oauth2.Credential{
				ClientID:     githubClientID,
				ClientSecret: githubClientSecret,
			},
			CallbackURL: endpointBase() + "/oauth/github/callback",
		},
		CSRFToken: "should_be_random",
	}
}

func endpointBase() string {
	scheme := "https"
	if mode == "development" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

func getUrlParams(router *httprouter.Router, req *http.Request) httprouter.Params {
	_, params, _ := router.Lookup(req.Method, req.URL.Path)
	return params
}

func callwithParams(router *httprouter.Router, handler func(w http.ResponseWriter, r *http.Request, ps httprouter.Params)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		params := getUrlParams(router, r)
		handler(w, r, params)
	}
}

func checkEnv() {
	fail := func(env string) {
		fmt.Printf("environment variable %s required\n", env)
		os.Exit(1)
	}
	if mode == "" {
		fail("APP_MODE")
	}
	if root == "" {
		fail("APP_ROOT")
	}
	if host == "" {
		fail("APP_HOST")
	}
	if port == "" {
		fail("APP_PORT")
	}
	if gcpProjectID == "" {
		fail("GCP_PROJECT_ID")
	}
	if gcpServiceAccountCredential == "" {
		fail("GCP_CREDENTIAL")
	}
	if githubClientID == "" {
		fail("GITHUB_CLIENT_ID")
	}
	if githubClientSecret == "" {
		fail("GITHUB_CLIENT_SECRET")
	}
}

func init() {
	mode = os.Getenv("APP_MODE")
	root = os.Getenv("APP_ROOT")
	host = os.Getenv("APP_HOST")
	port = os.Getenv("APP_PORT")
	gcpProjectID = os.Getenv("GCP_PROJECT_ID")
	gcpServiceAccountCredential = os.Getenv("GCP_CREDENTIAL")
	githubClientID = os.Getenv("GITHUB_CLIENT_ID")
	githubClientSecret = os.Getenv("GITHUB_CLIENT_SECRET")

	checkEnv()
}
