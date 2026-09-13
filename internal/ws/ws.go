package ws

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	mrand "math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	buzzWindow = 30 * time.Millisecond
	answerTime = 10 * time.Second
	maxNameLen = 32
)

type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

func newMessage(typ string, data any) Message {
	m := Message{Type: typ}
	if data != nil {
		m.Data, _ = json.Marshal(data)
	}
	return m
}

type conn struct {
	send   chan Message
	closed bool
	player *player
}

type player struct {
	token string
	name  string
	conn  *conn
}

type eventKind int

const (
	evMessage eventKind = iota
	evLeave
	evBuzzWindowClosed
	evAnswerTimeout
)

type event struct {
	kind eventKind
	conn *conn
	msg  Message
	at   time.Time
}

// Room state is only touched by the run goroutine; everything else goes through events.
type Room struct {
	events  chan event
	players map[string]*player

	buzzOpen   bool
	candidates []*player
	firstBuzz  time.Time
}

func NewRoom() *Room {
	r := &Room{
		events:   make(chan event, 64),
		players:  map[string]*player{},
		buzzOpen: true,
	}
	go r.run()
	return r
}

func (r *Room) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ws, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()

	ctx := req.Context()
	c := &conn{send: make(chan Message, 16)}

	go func() {
		for m := range c.send {
			if err := wsjson.Write(ctx, ws, m); err != nil {
				ws.CloseNow()
				return
			}
		}
		ws.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		var m Message
		if err := wsjson.Read(ctx, ws, &m); err != nil {
			break
		}
		r.events <- event{kind: evMessage, conn: c, msg: m, at: time.Now()}
	}
	r.events <- event{kind: evLeave, conn: c}
}

func (r *Room) run() {
	for e := range r.events {
		switch e.kind {
		case evMessage:
			r.handleMessage(e)
		case evLeave:
			r.drop(e.conn)
		case evBuzzWindowClosed:
			r.closeBuzzWindow()
		case evAnswerTimeout:
			r.buzzOpen = true
			r.broadcast(newMessage("buzz-available", nil))
		}
	}
}

func (r *Room) handleMessage(e event) {
	c := e.conn
	switch e.msg.Type {
	case "join":
		var d struct {
			Token string `json:"token"`
		}
		if !r.decode(c, e.msg, &d) {
			return
		}
		p := r.players[d.Token]
		if p == nil {
			p = &player{token: newToken()}
			r.players[p.token] = p
		}
		if c.player != nil && c.player != p && c.player.conn == c {
			c.player.conn = nil
		}
		if p.conn != nil && p.conn != c {
			r.drop(p.conn)
		}
		p.conn = c
		c.player = p
		r.send(c, newMessage("welcome", map[string]string{"token": p.token, "name": p.name}))

	case "identify":
		if c.player == nil {
			r.sendError(c, "not-joined", "join first")
			return
		}
		var d struct {
			Name string `json:"name"`
		}
		if !r.decode(c, e.msg, &d) {
			return
		}
		name := strings.TrimSpace(d.Name)
		if name == "" || utf8.RuneCountInString(name) > maxNameLen {
			r.sendError(c, "invalid-name", "name must be 1 to 32 characters")
			return
		}
		for _, p := range r.players {
			if p != c.player && strings.EqualFold(p.name, name) {
				r.sendError(c, "name-taken", "name already taken")
				return
			}
		}
		c.player.name = name
		r.broadcastLobby()

	case "buzz":
		p := c.player
		if p == nil || p.name == "" {
			r.sendError(c, "not-identified", "join and identify first")
			return
		}
		if !r.buzzOpen || slices.Contains(r.candidates, p) {
			return
		}
		if len(r.candidates) == 0 {
			r.firstBuzz = e.at
			time.AfterFunc(buzzWindow, func() { r.events <- event{kind: evBuzzWindowClosed} })
		} else if e.at.Sub(r.firstBuzz) > buzzWindow {
			// ponytail: a buzz received in-window but queued after the window closed is dropped; sub-millisecond race.
			return
		}
		r.candidates = append(r.candidates, p)

	default:
		r.sendError(c, "unknown-type", "unknown message type")
	}
}

func (r *Room) closeBuzzWindow() {
	if len(r.candidates) == 0 {
		return
	}
	winner := r.candidates[mrand.IntN(len(r.candidates))]
	r.candidates = nil
	r.buzzOpen = false

	r.broadcast(newMessage("buzz-accepted", map[string]any{
		"name":       winner.name,
		"answerTime": answerTime.Seconds(),
	}))
	blocked := newMessage("buzz-blocked", nil)
	for _, p := range r.players {
		if p != winner {
			r.send(p.conn, blocked)
		}
	}
	time.AfterFunc(answerTime, func() { r.events <- event{kind: evAnswerTimeout} })
}

func (r *Room) broadcastLobby() {
	names := []string{}
	for _, p := range r.players {
		if p.name != "" {
			names = append(names, p.name)
		}
	}
	slices.Sort(names)
	r.broadcast(newMessage("lobby-update", map[string][]string{"players": names}))
}

func (r *Room) decode(c *conn, m Message, v any) bool {
	if len(m.Data) == 0 {
		return true
	}
	if err := json.Unmarshal(m.Data, v); err != nil {
		r.sendError(c, "bad-data", "invalid data for "+m.Type)
		return false
	}
	return true
}

func (r *Room) send(c *conn, m Message) {
	if c == nil || c.closed {
		return
	}
	select {
	case c.send <- m:
	default:
		// Slow client: drop it, it reconnects with its token.
		r.drop(c)
	}
}

func (r *Room) broadcast(m Message) {
	for _, p := range r.players {
		r.send(p.conn, m)
	}
}

func (r *Room) sendError(c *conn, code, message string) {
	r.send(c, newMessage("error", map[string]string{"code": code, "message": message}))
}

func (r *Room) drop(c *conn) {
	if c.closed {
		return
	}
	c.closed = true
	close(c.send)
	if c.player != nil && c.player.conn == c {
		c.player.conn = nil
	}
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
