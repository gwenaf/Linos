package game

import (
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync/atomic"
	"time"
)

const (
	RoleControl = "control"
	RoleHost    = "host"
	RolePlayer  = "player"
)

type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

func NewMessage(typ string, data any) Message {
	m := Message{Type: typ}
	if data != nil {
		m.Data, _ = json.Marshal(data)
	}
	return m
}

type Client struct {
	role string
	// token is only set for gamemaster phones; players carry theirs in player.token.
	token  string
	send   chan Message
	joined bool
	closed bool
	player *player
}

// Messages is closed when the room drops the client.
func (c *Client) Messages() <-chan Message { return c.send }

type eventKind int

const (
	evMessage eventKind = iota
	evLeave
	evBuzzWindowClosed
	evAnswerTimeout
	evTrackTimer
)

type event struct {
	kind   eventKind
	client *Client
	msg    Message
	at     time.Time
	turn   int
}

// Room state is only touched by the run goroutine; everything else goes through events.
type Room struct {
	events  chan event
	clients map[*Client]struct{}
	players map[string]*player
	teams   []*team
	state   string

	masters   map[string]bool
	invites   map[string]time.Time
	inviteTTL time.Duration
	// tolerated players may stay disconnected without pausing the game, until they reconnect.
	tolerated map[*player]bool

	packsDir string
	// loaded is also read by HTTP handlers serving media, hence atomic.
	loaded     atomic.Pointer[loadedPack]
	playlist   []playItem
	current    int
	rules      rules
	trackState string
	found      map[string]bool
	endReason  string
	// Answers on phones: who answered which guess in simultaneous mode, how many got each right (rank scoring),
	// points revealed at the end of the track, and whether the buzz holder already sent an answer.
	answered       map[string]map[any]bool
	correctCount   map[string]int
	pendingPoints  []scored
	holderAnswered bool
	// trackSeq invalidates track timers from previous tracks.
	trackSeq int

	buzzOpen   bool
	candidates []*player
	firstBuzz  time.Time
	holder     *player
	attempted  map[any]bool

	trackStart    time.Time
	paused        time.Duration
	pausedAt      time.Time
	clockStops    int
	holderElapsed time.Duration
	// turn invalidates pending timers whenever the buzz state moves on.
	turn int
}

func NewRoom(packsDir string) *Room {
	r := &Room{
		events:    make(chan event, 64),
		clients:   map[*Client]struct{}{},
		players:   map[string]*player{},
		state:     stateLobby,
		masters:   map[string]bool{},
		invites:   map[string]time.Time{},
		inviteTTL: 2 * time.Minute,
		tolerated: map[*player]bool{},
		attempted: map[any]bool{},
		packsDir:  packsDir,
	}
	go r.run()
	return r
}

func (r *Room) Connect(role string) *Client {
	return &Client{role: role, send: make(chan Message, 64)}
}

func (r *Room) Receive(c *Client, m Message) {
	r.events <- event{kind: evMessage, client: c, msg: m, at: time.Now()}
}

func (r *Room) Disconnect(c *Client) {
	r.events <- event{kind: evLeave, client: c}
}

func (r *Room) run() {
	defer logPanic()
	for e := range r.events {
		switch e.kind {
		case evMessage:
			r.handle(e)
		case evLeave:
			r.drop(e.client)
		case evBuzzWindowClosed:
			if e.turn == r.turn {
				r.closeBuzzWindow()
			}
		case evAnswerTimeout:
			if e.turn == r.turn {
				r.resolveAnswer(false, nil)
			}
		case evTrackTimer:
			r.trackTimer(e.turn)
		}
	}
}

var (
	lobbyStates   = []string{stateLobby, stateReady}
	runningStates = []string{stateInProgress, statePaused, stateTechnicalPause}
)

// commands lists, for each message, the role allowed to send it and the states it is accepted in (nil: any).
var commands = map[string]struct {
	role   string
	states []string
}{
	"identify":      {RolePlayer, nil},
	"join-team":     {RolePlayer, lobbyStates},
	"ready":         {RolePlayer, lobbyStates},
	"buzz":          {RolePlayer, []string{stateInProgress}},
	"answer":        {RolePlayer, []string{stateInProgress}},
	"media-started": {RoleHost, []string{stateInProgress}},
	"list-packs":    {RoleControl, nil},
	"configure":     {RoleControl, lobbyStates},
	"start-game":    {RoleControl, []string{stateReady}},
	"pause":         {RoleControl, []string{stateInProgress}},
	"resume":        {RoleControl, []string{statePaused, stateTechnicalPause}},
	"validate":      {RoleControl, runningStates},
	"skip":          {RoleControl, []string{stateInProgress}},
	"score-adjust":  {RoleControl, nil},
	"kick":          {RoleControl, nil},
	"master-invite": {RoleControl, nil},
	"end-game":      {RoleControl, runningStates},
	"abort":         {RoleControl, runningStates},
}

func (r *Room) handle(e event) {
	c, typ := e.client, e.msg.Type
	// The transport may still deliver messages read before the room dropped the client.
	if c.closed {
		return
	}
	slog.Debug("message received", append(c.logAttrs(), "type", typ)...)
	if typ == "join" {
		r.join(c, e.msg)
		return
	}
	cmd, ok := commands[typ]
	switch {
	case !ok:
		r.sendError(c, "unknown-type", "unknown message type")
		return
	case !c.joined:
		r.sendError(c, "not-joined", "join first")
		return
	case c.role != cmd.role:
		r.sendError(c, "forbidden", typ+" is not allowed for "+c.role)
		return
	case cmd.states != nil && !slices.Contains(cmd.states, r.state):
		r.sendError(c, "wrong-state", typ+" is not allowed in state "+r.state)
		return
	}

	switch typ {
	case "identify":
		r.identify(c, e.msg)
	case "join-team":
		r.joinTeam(c, e.msg)
	case "ready":
		r.setReady(c, e.msg)
	case "buzz":
		r.buzz(c, e.at)
	case "answer":
		r.answer(c, e.msg, e.at)
	case "media-started":
		r.mediaStarted(c)
	case "list-packs":
		r.listPacks(c)
	case "configure":
		r.configure(c, e.msg)
	case "start-game":
		r.startGame(c)
	case "pause":
		r.freeze(statePaused)
		r.broadcast(NewMessage("game-paused", r.pausedPayload()))
	case "resume":
		r.resume()
	case "validate":
		r.validate(c, e.msg)
	case "skip":
		r.skip()
	case "score-adjust":
		r.scoreAdjust(c, e.msg)
	case "kick":
		r.kick(c, e.msg)
	case "master-invite":
		r.createInvite(c)
	case "end-game":
		r.endGame("ended")
	case "abort":
		r.endGame("aborted")
	}
}

func (r *Room) join(c *Client, m Message) {
	var d struct {
		Token  string `json:"token"`
		Invite string `json:"invite"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if c.role == RolePlayer {
		switch {
		case d.Invite != "":
			expiry, ok := r.invites[d.Invite]
			delete(r.invites, d.Invite)
			if !ok || time.Now().After(expiry) {
				slog.Warn("gamemaster invite refused", "known", ok)
				r.sendError(c, "invalid-invite", "invite unknown or expired")
				return
			}
			r.detach(c)
			c.role, c.token = RoleControl, newToken()
			r.masters[c.token] = true
		case r.masters[d.Token]:
			r.detach(c)
			c.role, c.token = RoleControl, d.Token
		default:
			r.joinPlayer(c, d.Token)
		}
	}
	c.joined = true
	r.clients[c] = struct{}{}

	welcome := map[string]any{"role": c.role, "state": r.state}
	if c.token != "" {
		welcome["token"] = c.token
	}
	if c.player != nil {
		welcome["token"] = c.player.token
		welcome["name"] = c.player.name
	}
	slog.Info("client joined", append(c.logAttrs(), "state", r.state)...)
	r.send(c, NewMessage("welcome", welcome))
	r.send(c, NewMessage("state", r.snapshot(c)))
	r.connectionsChanged()
}

func (r *Room) decode(c *Client, m Message, v any) bool {
	if len(m.Data) == 0 {
		return true
	}
	if err := json.Unmarshal(m.Data, v); err != nil {
		r.sendError(c, "bad-data", "invalid data for "+m.Type)
		return false
	}
	return true
}

func (r *Room) send(c *Client, m Message) {
	if c == nil || c.closed {
		return
	}
	select {
	case c.send <- m:
	default:
		// Slow client: drop it, it reconnects with its token.
		slog.Warn("slow client dropped", c.logAttrs()...)
		r.drop(c)
	}
}

func (r *Room) broadcast(m Message) {
	for c := range r.clients {
		r.send(c, m)
	}
}

func (r *Room) sendError(c *Client, code, message string) {
	slog.Info("error sent", append(c.logAttrs(), "code", code, "message", message)...)
	r.send(c, NewMessage("error", map[string]string{"code": code, "message": message}))
}

func (r *Room) drop(c *Client) {
	if c.closed {
		return
	}
	slog.Info("client left", c.logAttrs()...)
	c.closed = true
	close(c.send)
	delete(r.clients, c)
	r.detach(c)
	r.connectionsChanged()
}

// detach unlinks a client from its player, leaving the player disconnected unless another device took over.
func (r *Room) detach(c *Client) {
	if c.player != nil && c.player.client == c {
		c.player.client = nil
	}
	c.player = nil
}

// logAttrs identifies a client in logs, never with its token.
func (c *Client) logAttrs() []any {
	if c.player != nil {
		return []any{"role", c.role, "player", c.player.name}
	}
	return []any{"role", c.role}
}

// logPanic writes a crash to the journal before letting it stop the program: the room state cannot be trusted anymore.
func logPanic() {
	if v := recover(); v != nil {
		slog.Error("room crashed", "panic", v, "stack", string(debug.Stack()))
		panic(v)
	}
}
