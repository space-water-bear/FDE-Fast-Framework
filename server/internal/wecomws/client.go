// Package wecomws implements the intelligent bot WebSocket transport. Protocol
// reference: https://github.com/WecomTeam/aibot-node-sdk/blob/main/src/ws.ts
package wecomws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	randv2 "math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

const DefaultURL = "wss://openws.work.weixin.qq.com"

const (
	callbackReplyWindow    = 24 * time.Hour
	streamReplyWindow      = 10 * time.Minute
	maxRegisteredStreams   = 65536
	maxRegisteredCallbacks = 65536
)

var (
	ErrConfiguration  = errors.New("invalid WeCom WebSocket configuration")
	ErrAuthFailed     = errors.New("WeCom WebSocket authentication failed")
	ErrSuperseded     = errors.New("WeCom WebSocket superseded by another connection")
	ErrDisconnected   = errors.New("WeCom WebSocket disconnected")
	ErrAckTimeout     = errors.New("WeCom WebSocket acknowledgement timed out")
	ErrRejected       = errors.New("WeCom WebSocket reply rejected")
	ErrQueueFull      = errors.New("WeCom WebSocket reply queue full")
	ErrStaleRequest   = errors.New("WeCom WebSocket request does not belong to this connection")
	ErrInvalidReply   = errors.New("invalid WeCom WebSocket reply")
	ErrStreamFinished = errors.New("WeCom WebSocket stream has already finished")
	ErrStreamExpired  = errors.New("WeCom WebSocket stream reply window expired")
	ErrStreamBinding  = errors.New("WeCom WebSocket stream belongs to another callback")
)

type Config struct {
	BotID, Secret, URL                                        string
	HeartbeatInterval, AckTimeout, ReconnectMin, ReconnectMax time.Duration
}

type Headers struct {
	ReqID string `json:"req_id"`
}

type Frame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers Headers         `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode *int            `json:"errcode,omitempty"`
	ErrMsg  string          `json:"errmsg,omitempty"`
}

type Status struct {
	State                            string
	ReconnectAttempt                 int
	LastConnectedAt, LastHeartbeatAt time.Time
}

// Client has one Run lifecycle. Message/status callbacks must return promptly.
// Message callbacks run outside the read loop and may call ReplyStream.
type Client struct {
	cfg             Config
	onMessage       func(Frame)
	onStatus        func(Status)
	mu              sync.Mutex
	active          *session
	cancel          context.CancelFunc
	started         bool
	closed          bool
	status          Status
	generation      uint64
	streams         map[string]streamState
	lastStreamSweep time.Time
	// Tests in this package inject a local dialer. Public URL configuration only
	// accepts the official endpoint and cannot redirect credentials elsewhere.
	dial func(context.Context) (*websocket.Conn, error)
}

func NewClient(cfg Config, onMessage func(Frame), onStatus func(Status)) *Client {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = 5 * time.Second
	}
	if cfg.ReconnectMin <= 0 {
		cfg.ReconnectMin = time.Second
	}
	if cfg.ReconnectMax <= 0 {
		cfg.ReconnectMax = 30 * time.Second
	}
	if cfg.ReconnectMax < cfg.ReconnectMin {
		cfg.ReconnectMax = cfg.ReconnectMin
	}
	return &Client{cfg: cfg, onMessage: onMessage, onStatus: onStatus, streams: make(map[string]streamState)}
}

func (c *Client) publish(state string, attempt int) {
	c.mu.Lock()
	c.status.State, c.status.ReconnectAttempt = state, attempt
	if state == "connected" {
		c.status.LastConnectedAt = time.Now()
	}
	if state == "heartbeat" {
		c.status.State = "connected"
		c.status.LastHeartbeatAt = time.Now()
	}
	status := c.status
	c.mu.Unlock()
	if c.onStatus != nil {
		c.onStatus(status)
	}
}

// Close cancels Run, closes the socket and releases pending replies promptly.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// Run reconnects transient failures indefinitely. Explicit authentication failure
// and a disconnected_event are terminal; neither is retried automatically.
func (c *Client) Run(ctx context.Context) error {
	c.mu.Lock()
	if c.started || c.closed {
		c.mu.Unlock()
		return ErrDisconnected
	}
	c.started = true
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()
	defer cancel()
	if c.cfg.BotID == "" || c.cfg.Secret == "" || (c.cfg.URL != DefaultURL && c.cfg.URL != DefaultURL+"/") {
		c.publish("auth_failed", 0)
		return ErrConfiguration
	}
	attempt := 0
	for {
		if ctx.Err() != nil {
			c.publish("stopped", 0)
			return ctx.Err()
		}
		c.publish("connecting", attempt)
		authenticated, err := c.runConnection(ctx)
		if ctx.Err() != nil {
			c.publish("stopped", 0)
			return ctx.Err()
		}
		if errors.Is(err, ErrAuthFailed) {
			c.publish("auth_failed", 0)
			return ErrAuthFailed
		}
		if errors.Is(err, ErrSuperseded) {
			c.publish("superseded", 0)
			return ErrSuperseded
		}
		if authenticated {
			attempt = 0
		}
		attempt++
		c.publish("reconnecting", attempt)
		delay := float64(c.cfg.ReconnectMin) * math.Pow(2, float64(min(attempt-1, 30)))
		delay = min(delay, float64(c.cfg.ReconnectMax))
		delay = min(delay*(0.8+randv2.Float64()*0.4), float64(c.cfg.ReconnectMax))
		timer := time.NewTimer(time.Duration(delay))
		select {
		case <-ctx.Done():
			timer.Stop()
			c.publish("stopped", 0)
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type replyRequest struct {
	ctx      context.Context
	frame    Frame
	result   chan error
	streamID string
	finish   bool
}

// Stream state outlives a session so a callback repeated on a replacement
// connection cannot accidentally replay an uncertain write from the old one.
type streamState struct {
	reqID      string
	generation uint64
	startedAt  time.Time
	finished   bool
}

type session struct {
	conn       *websocket.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	outbox     chan replyRequest
	authed     atomic.Bool
	knownMu    sync.Mutex
	known      map[string]time.Time
	generation uint64
}

// ReplyStream never replays an uncertain write, and never sends a callback's
// req_id on a later connection. It waits for an explicit errcode=0 ACK.
func (c *Client) ReplyStream(ctx context.Context, reqID, streamID, content string, finish bool) error {
	if reqID == "" || streamID == "" || len(reqID) > 1024 || len(streamID) > 1024 || len(content) > 20480 || !utf8.ValidString(content) {
		return ErrInvalidReply
	}
	c.mu.Lock()
	s := c.active
	c.mu.Unlock()
	if s == nil || !s.authed.Load() {
		return ErrDisconnected
	}
	s.knownMu.Lock()
	received, exists := s.known[reqID]
	s.knownMu.Unlock()
	if !exists || !time.Now().Before(received.Add(callbackReplyWindow)) {
		return ErrStaleRequest
	}
	body, _ := json.Marshal(map[string]any{"msgtype": "stream", "stream": map[string]any{"id": streamID, "content": content, "finish": finish}})
	req := replyRequest{ctx: ctx, frame: Frame{Cmd: "aibot_respond_msg", Headers: Headers{ReqID: reqID}, Body: body}, result: make(chan error, 1), streamID: streamID, finish: finish}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return ErrDisconnected
	case s.outbox <- req:
	}
	select {
	case err := <-req.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		select {
		case err := <-req.result:
			return err
		default:
			return ErrDisconnected
		}
	}
}

type readResult struct {
	frame Frame
	err   error
}

func readFrames(ctx context.Context, conn *websocket.Conn, out chan<- readResult) {
	for {
		typ, data, err := conn.Read(ctx)
		var frame Frame
		if err == nil && (typ != websocket.MessageText || json.Unmarshal(data, &frame) != nil) {
			err = ErrDisconnected
		}
		select {
		case out <- readResult{frame: frame, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func requestID(prefix string) string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		// An entropy failure must not expose credentials or create a reused ACK ID.
		return ""
	}
	return prefix + "_" + hex.EncodeToString(random)
}

func (c *Client) dialConnection(ctx context.Context) (*websocket.Conn, error) {
	if c.dial != nil {
		return c.dial(ctx)
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(dialCtx, c.cfg.URL, &websocket.DialOptions{
		HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	})
	if response != nil && err != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, ErrDisconnected
	}
	return conn, nil
}

func (c *Client) write(s *session, frame Frame) error {
	ctx, cancel := context.WithTimeout(s.ctx, c.cfg.AckTimeout)
	defer cancel()
	return writeFrame(ctx, s, frame)
}

func (c *Client) writeReply(s *session, req replyRequest) error {
	ctx, cancel := context.WithTimeout(req.ctx, c.cfg.AckTimeout)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	defer func() { stopSessionCancel(); cancel() }()
	if s.ctx.Err() != nil {
		return ErrDisconnected
	}
	return writeFrame(ctx, s, req.frame)
}

func writeFrame(ctx context.Context, s *session, frame Frame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return ErrDisconnected
	}
	if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return ErrDisconnected
	}
	return nil
}

// Called only immediately before writing a dequeued reply. Queueing does not
// start the ten-minute stream clock, and queued frames must observe a final ACK
// or window expiry that happened while waiting behind an earlier reply.
func (c *Client) prepareReply(s *session, req replyRequest) error {
	if err := req.ctx.Err(); err != nil {
		return err
	}
	if s.ctx.Err() != nil {
		return ErrDisconnected
	}
	now := time.Now()
	s.knownMu.Lock()
	received, known := s.known[req.frame.Headers.ReqID]
	s.knownMu.Unlock()
	if !known || !now.Before(received.Add(callbackReplyWindow)) {
		return ErrStaleRequest
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if state, ok := c.streams[req.streamID]; ok {
		if state.reqID != req.frame.Headers.ReqID {
			return ErrStreamBinding
		}
		if state.finished {
			return ErrStreamFinished
		}
		if state.generation != s.generation {
			return ErrStaleRequest
		}
		if !now.Before(state.startedAt.Add(streamReplyWindow)) {
			return ErrStreamExpired
		}
		return nil
	}
	if now.Sub(c.lastStreamSweep) >= time.Minute || len(c.streams) >= maxRegisteredStreams {
		for id, state := range c.streams {
			if !now.Before(state.startedAt.Add(callbackReplyWindow)) {
				delete(c.streams, id)
			}
		}
		c.lastStreamSweep = now
	}
	if len(c.streams) >= maxRegisteredStreams {
		return ErrQueueFull
	}
	c.streams[req.streamID] = streamState{reqID: req.frame.Headers.ReqID, generation: s.generation, startedAt: now}
	return nil
}

func (c *Client) acknowledgeReply(s *session, req replyRequest) {
	if !req.finish {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.streams[req.streamID]
	if ok && state.reqID == req.frame.Headers.ReqID && state.generation == s.generation {
		state.finished = true
		c.streams[req.streamID] = state
	}
}

func disconnectedEvent(frame Frame) bool {
	if frame.Cmd != "aibot_event_callback" {
		return false
	}
	var body struct {
		Event struct {
			Type string `json:"eventtype"`
		} `json:"event"`
	}
	return json.Unmarshal(frame.Body, &body) == nil && body.Event.Type == "disconnected_event"
}

type pendingReply struct {
	req      replyRequest
	deadline time.Time
}

func (c *Client) runConnection(ctx context.Context) (authenticated bool, returnedErr error) {
	conn, err := c.dialConnection(ctx)
	if err != nil {
		return false, ErrDisconnected
	}
	conn.SetReadLimit(1 << 20)
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &session{conn: conn, ctx: sessionCtx, cancel: cancel, done: make(chan struct{}), outbox: make(chan replyRequest, 64), known: make(map[string]time.Time)}
	c.mu.Lock()
	c.generation++
	s.generation = c.generation
	c.active = s
	c.mu.Unlock()
	pending := make(map[string]pendingReply)
	queues := make(map[string][]replyRequest)
	defer func() {
		s.authed.Store(false)
		cancel()
		_ = conn.CloseNow()
		c.mu.Lock()
		if c.active == s {
			c.active = nil
		}
		c.mu.Unlock()
		failure := returnedErr
		if failure == nil {
			failure = ErrDisconnected
		}
		for _, p := range pending {
			p.req.result <- failure
		}
		for _, queue := range queues {
			for _, req := range queue {
				req.result <- failure
			}
		}
		s.knownMu.Lock()
		clear(s.known)
		s.knownMu.Unlock()
		close(s.done)
	}()
	incoming := make(chan readResult, 32)
	go readFrames(sessionCtx, conn, incoming)
	callbacks := make(chan Frame, 64)
	go func() {
		for {
			select {
			case <-sessionCtx.Done():
				return
			case frame := <-callbacks:
				if sessionCtx.Err() == nil && c.onMessage != nil {
					c.onMessage(frame)
				}
			}
		}
	}()
	c.publish("authenticating", 0)
	authID := requestID("aibot_subscribe")
	if authID == "" {
		return false, ErrDisconnected
	}
	body, _ := json.Marshal(map[string]string{"bot_id": c.cfg.BotID, "secret": c.cfg.Secret})
	if err := c.write(s, Frame{Cmd: "aibot_subscribe", Headers: Headers{ReqID: authID}, Body: body}); err != nil {
		return false, err
	}
	authTimer := time.NewTimer(c.cfg.AckTimeout)
	defer authTimer.Stop()
authLoop:
	for {
		select {
		case <-sessionCtx.Done():
			return false, ErrDisconnected
		case <-authTimer.C:
			return false, ErrAckTimeout
		case result := <-incoming:
			if result.err != nil {
				return false, ErrDisconnected
			}
			if disconnectedEvent(result.frame) {
				return false, ErrSuperseded
			}
			if result.frame.Cmd == "" && result.frame.Headers.ReqID == authID && result.frame.ErrCode != nil {
				if *result.frame.ErrCode != 0 {
					return false, ErrAuthFailed
				}
				break authLoop
			}
		}
	}
	authenticated = true
	s.authed.Store(true)
	c.publish("connected", 0)
	heartbeat := time.NewTicker(c.cfg.HeartbeatInterval)
	defer heartbeat.Stop()
	sweep := time.NewTicker(max(time.Millisecond, min(250*time.Millisecond, c.cfg.AckTimeout/4)))
	defer sweep.Stop()
	pings := make(map[string]time.Time)
	missedPings, outstanding := 0, 0
	start := func(req replyRequest) (bool, error) {
		if err := c.prepareReply(s, req); err != nil {
			outstanding--
			req.result <- err
			return false, nil
		}
		pending[req.frame.Headers.ReqID] = pendingReply{req: req, deadline: time.Now().Add(c.cfg.AckTimeout)}
		return true, c.writeReply(s, req)
	}
	advance := func(reqID string) error {
		queue := queues[reqID]
		for len(queue) != 0 {
			req := queue[0]
			queue = queue[1:]
			queues[reqID] = queue
			if err := req.ctx.Err(); err != nil {
				outstanding--
				req.result <- err
				continue
			}
			sent, err := start(req)
			if err != nil || sent {
				return err
			}
		}
		delete(queues, reqID)
		return nil
	}
	for {
		select {
		case <-sessionCtx.Done():
			return true, ErrDisconnected
		case req := <-s.outbox:
			if err := req.ctx.Err(); err != nil {
				req.result <- err
				continue
			}
			reqID := req.frame.Headers.ReqID
			if outstanding >= 128 || len(queues[reqID]) >= 32 {
				req.result <- ErrQueueFull
				continue
			}
			outstanding++
			if _, exists := pending[reqID]; exists {
				queues[reqID] = append(queues[reqID], req)
			} else {
				if _, err := start(req); err != nil {
					return true, err
				}
			}
		case result := <-incoming:
			if result.err != nil {
				return true, ErrDisconnected
			}
			frame := result.frame
			if disconnectedEvent(frame) {
				return true, ErrSuperseded
			}
			if frame.Cmd == "aibot_msg_callback" || frame.Cmd == "aibot_event_callback" {
				if frame.Headers.ReqID == "" || len(frame.Headers.ReqID) > 1024 {
					continue
				}
				s.knownMu.Lock()
				for id, received := range s.known {
					if !time.Now().Before(received.Add(callbackReplyWindow)) {
						delete(s.known, id)
					}
				}
				_, exists := s.known[frame.Headers.ReqID]
				full := !exists && len(s.known) >= maxRegisteredCallbacks
				if !full && !exists {
					s.known[frame.Headers.ReqID] = time.Now()
				}
				s.knownMu.Unlock()
				if full {
					return true, ErrQueueFull
				}
				select {
				case callbacks <- frame:
				default:
					return true, ErrQueueFull
				}
				continue
			}
			if frame.Cmd != "" || frame.ErrCode == nil {
				continue
			}
			reqID := frame.Headers.ReqID
			if _, exists := pings[reqID]; exists {
				delete(pings, reqID)
				if *frame.ErrCode == 0 {
					missedPings = 0
					c.publish("heartbeat", 0)
				} else {
					missedPings++
					if missedPings >= 2 {
						return true, ErrAckTimeout
					}
				}
				continue
			}
			if p, exists := pending[reqID]; exists {
				delete(pending, reqID)
				outstanding--
				var err error
				if *frame.ErrCode != 0 {
					err = ErrRejected
				} else {
					c.acknowledgeReply(s, p.req)
				}
				p.req.result <- err
				if err := advance(reqID); err != nil {
					return true, err
				}
			}
		case <-heartbeat.C:
			id := requestID("ping")
			if id == "" {
				return true, ErrDisconnected
			}
			pings[id] = time.Now().Add(c.cfg.AckTimeout)
			if err := c.write(s, Frame{Cmd: "ping", Headers: Headers{ReqID: id}}); err != nil {
				return true, err
			}
		case now := <-sweep.C:
			for _, p := range pending {
				if !now.Before(p.deadline) {
					// A late ACK cannot be distinguished from the next reply with the
					// same req_id. Reconnect instead of sending the queued next frame.
					return true, ErrAckTimeout
				}
			}
			for id, deadline := range pings {
				if !now.Before(deadline) {
					delete(pings, id)
					missedPings++
				}
			}
			if missedPings >= 2 {
				return true, ErrAckTimeout
			}
		}
	}
}
