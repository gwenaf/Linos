package ws

import (
	"net/http"

	"github.com/coder/websocket"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx := r.Context()
	for {
		typ, msg, err := c.Read(ctx)
		if err != nil {
			return
		}
		if err := c.Write(ctx, typ, msg); err != nil {
			return
		}
	}
}
