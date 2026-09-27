package bad

import (
	"net/http"

	"github.com/coder/websocket"
)

func run(w http.ResponseWriter, r *http.Request) { _, _ = websocket.Accept(w, r, nil) }
