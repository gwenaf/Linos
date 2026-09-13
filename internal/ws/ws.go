package ws

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math"
	mrand "math/rand/v2"
	"net"
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

// Manifest defaults until pack rules are loaded.
var scoring = struct {
	max, min     int
	duration     time.Duration
	wrongPenalty int
}{max: 100, min: 20, duration: 30 * time.Second}

const (
	roleControl = "control"
	roleHost    = "host"
	rolePlayer  = "player"
)

var requiredRole = map[string]string{
	"identify":     rolePlayer,
	"buzz":         rolePlayer,
	"validate":     roleControl,
	"skip":         roleControl,
	"score-adjust": roleControl,
}

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
	role   string
	send   chan Message
	joined bool
	closed bool
	player *player
}

type player struct {
	token string
	name  string
	score int
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
	turn int
}

// Room state is only touched by the run goroutine; everything else goes through events.
type Room struct {
	events  chan event
	conns   map[*conn]struct{}
	players map[string]*player

	buzzOpen   bool
	candidates []*player
	firstBuzz  time.Time
	holder     *player
	attempted  map[*player]bool

	trackStart    time.Time
	paused        time.Duration
	pausedAt      time.Time
	holderElapsed time.Duration
	// turn invalidates pending timers whenever the buzz state moves on.
	turn int
}

func NewRoom() *Room {
	r := &Room{
		events:     make(chan event, 64),
		conns:      map[*conn]struct{}{},
		players:    map[string]*player{},
		buzzOpen:   true,
		attempted:  map[*player]bool{},
		trackStart: time.Now(),
	}
	go r.run()
	return r
}

func (r *Room) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	role := rolePlayer
	switch q := req.URL.Query().Get("role"); q {
	case "", rolePlayer:
	case roleControl, roleHost:
		if !isLocal(req) {
			http.Error(w, "role reserved to the host machine", http.StatusForbidden)
			return
		}
		role = q
	default:
		http.Error(w, "unknown role", http.StatusBadRequest)
		return
	}

	ws, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()

	ctx := req.Context()
	c := &conn{role: role, send: make(chan Message, 16)}

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

// isLocal also checks the Host header so a DNS-rebinding page opened on the host PC cannot claim control.
func isLocal(req *http.Request) bool {
	remote, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		return false
	}
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func (r *Room) run() {
	for e := range r.events {
		switch e.kind {
		case evMessage:
			r.handleMessage(e)
		case evLeave:
			r.drop(e.conn)
		case evBuzzWindowClosed:
			if e.turn == r.turn {
				r.closeBuzzWindow()
			}
		case evAnswerTimeout:
			if e.turn == r.turn && r.holder != nil {
				r.resolveAnswer(false)
			}
		}
	}
}

func (r *Room) handleMessage(e event) {
	c := e.conn
	if e.msg.Type != "join" && !c.joined {
		r.sendError(c, "not-joined", "join first")
		return
	}
	if want, ok := requiredRole[e.msg.Type]; ok && c.role != want {
		r.sendError(c, "forbidden", e.msg.Type+" is not allowed for "+c.role)
		return
	}

	switch e.msg.Type {
	case "join":
		if c.role == rolePlayer && !r.joinPlayer(c, e.msg) {
			return
		}
		c.joined = true
		r.conns[c] = struct{}{}
		welcome := map[string]string{"role": c.role}
		if c.player != nil {
			welcome["token"] = c.player.token
			welcome["name"] = c.player.name
		}
		r.send(c, newMessage("welcome", welcome))

	case "identify":
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
		if p.name == "" {
			r.sendError(c, "not-identified", "identify first")
			return
		}
		if !r.buzzOpen || r.attempted[p] || slices.Contains(r.candidates, p) {
			return
		}
		if len(r.candidates) == 0 {
			r.firstBuzz = e.at
			turn := r.turn
			time.AfterFunc(buzzWindow, func() { r.events <- event{kind: evBuzzWindowClosed, turn: turn} })
		} else if e.at.Sub(r.firstBuzz) > buzzWindow {
			// ponytail: a buzz received in-window but queued after the window closed is dropped; sub-millisecond race.
			return
		}
		r.candidates = append(r.candidates, p)

	case "validate":
		if r.holder == nil {
			r.sendError(c, "no-pending-answer", "no answer to validate")
			return
		}
		var d struct {
			Correct *bool `json:"correct"`
		}
		if !r.decode(c, e.msg, &d) {
			return
		}
		if d.Correct == nil {
			r.sendError(c, "bad-data", "validate requires correct")
			return
		}
		r.resolveAnswer(*d.Correct)

	case "skip":
		r.holder = nil
		r.candidates = nil
		r.attempted = map[*player]bool{}
		r.turn++
		r.trackStart = time.Now()
		r.paused = 0
		r.openBuzz()

	case "score-adjust":
		var d struct {
			Name  string `json:"name"`
			Delta int    `json:"delta"`
		}
		if !r.decode(c, e.msg, &d) {
			return
		}
		p := r.playerByName(d.Name)
		if p == nil {
			r.sendError(c, "unknown-player", "no player named "+d.Name)
			return
		}
		p.score += d.Delta
		r.broadcastScore(p)

	default:
		r.sendError(c, "unknown-type", "unknown message type")
	}
}

func (r *Room) joinPlayer(c *conn, m Message) bool {
	var d struct {
		Token string `json:"token"`
	}
	if !r.decode(c, m, &d) {
		return false
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
	return true
}

func (r *Room) closeBuzzWindow() {
	if len(r.candidates) == 0 {
		return
	}
	winner := r.candidates[mrand.IntN(len(r.candidates))]
	r.candidates = nil
	r.buzzOpen = false
	r.holder = winner
	r.attempted[winner] = true
	r.turn++
	// Candidates tied inside the window share the first buzz time; the clock pauses while the holder answers.
	r.holderElapsed = r.firstBuzz.Sub(r.trackStart) - r.paused
	r.pausedAt = time.Now()

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
	turn := r.turn
	time.AfterFunc(answerTime, func() { r.events <- event{kind: evAnswerTimeout, turn: turn} })
}

func (r *Room) resolveAnswer(correct bool) {
	p := r.holder
	r.holder = nil
	r.turn++
	r.paused += time.Since(r.pausedAt)

	points := -scoring.wrongPenalty
	if correct {
		points = speedPoints(scoring.max, scoring.min, scoring.duration, r.holderElapsed)
	}
	p.score += points
	r.broadcast(newMessage("answer-result", map[string]any{"name": p.name, "correct": correct, "points": points}))
	if points != 0 {
		r.broadcastScore(p)
	}
	if !correct {
		r.openBuzz()
	}
}

// speedPoints decreases linearly from hi at 0 to lo at duration, then stays at lo.
func speedPoints(hi, lo int, duration, elapsed time.Duration) int {
	f := min(max(float64(elapsed)/float64(duration), 0), 1)
	return hi - int(math.Round(float64(hi-lo)*f))
}

func (r *Room) broadcastScore(p *player) {
	r.broadcast(newMessage("score-update", map[string]any{"name": p.name, "score": p.score}))
}

func (r *Room) playerByName(name string) *player {
	for _, p := range r.players {
		if p.name != "" && p.name == name {
			return p
		}
	}
	return nil
}

func (r *Room) openBuzz() {
	r.buzzOpen = true
	available := newMessage("buzz-available", nil)
	for c := range r.conns {
		if c.player == nil || !r.attempted[c.player] {
			r.send(c, available)
		}
	}
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
	for c := range r.conns {
		r.send(c, m)
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
	delete(r.conns, c)
	if c.player != nil && c.player.conn == c {
		c.player.conn = nil
	}
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
