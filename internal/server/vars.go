package server

import (
	"net/http"
	"sync"

	"github.com/henvic/httpretty"
	"github.com/mbndr/logo"
	"github.com/mubeng/mubeng/internal/ratelimiter"
)

var (
	handler      *Proxy
	server       *http.Server
	dump         *httpretty.Logger
	mime         = "text/plain"
	log          *logo.Logger
	ok           = 1
	currentProxy string
	limiter      *ratelimiter.Limiter

	mutex    = sync.Mutex{}
	rotateMu = sync.Mutex{}
)
