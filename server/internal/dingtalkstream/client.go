// Package dingtalkstream implements the robot subset of DingTalk Stream.
// Protocol: https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/
package dingtalkstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	randv2 "math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	OpenURL    = "https://api.dingtalk.com/v1.0/gateway/connections/open"
	RobotTopic = "/v1.0/im/bot/messages/get"
)

var (
	ErrConfiguration = errors.New("invalid DingTalk Stream configuration")
	ErrAuthFailed    = errors.New("DingTalk Stream application authentication failed")
	ErrDisconnected  = errors.New("DingTalk Stream disconnected")
	ErrProtocol      = errors.New("invalid DingTalk Stream protocol response")
	ErrEndpoint      = errors.New("invalid DingTalk Stream endpoint")
)

type Config struct {
	ClientID, ClientSecret                 string
	AckTimeout, ReconnectMin, ReconnectMax time.Duration
	// IdleTimeout is a local keepalive policy, not an official server ping rate.
	IdleTimeout time.Duration
}

type Frame struct {
	SpecVersion string            `json:"specVersion,omitempty"`
	Type        string            `json:"type"`
	Headers     map[string]string `json:"headers"`
	Data        string            `json:"data"`
}

// UnmarshalJSON accepts numeric metadata such as headers.time, which is a
// Number in the official schema but a string in its examples. Routing fields
// remain strings: coercing a numeric messageId or topic would change identity.
func (f *Frame) UnmarshalJSON(data []byte) error {
	var raw struct {
		SpecVersion string                     `json:"specVersion,omitempty"`
		Type        string                     `json:"type"`
		Headers     map[string]json.RawMessage `json:"headers"`
		Data        string                     `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ErrProtocol
	}
	var headers map[string]string
	if raw.Headers != nil {
		headers = make(map[string]string, len(raw.Headers))
	}
	for key, value := range raw.Headers {
		trimmed := bytes.TrimSpace(value)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			var text string
			if err := json.Unmarshal(trimmed, &text); err != nil {
				return ErrProtocol
			}
			headers[key] = text
			continue
		}
		if key == "topic" || key == "messageId" || key == "contentType" {
			return ErrProtocol
		}
		// json.Number retains large integers and exponents without float64 loss.
		var number json.Number
		if err := json.Unmarshal(trimmed, &number); err != nil || number == "" {
			return ErrProtocol
		}
		headers[key] = number.String()
	}
	*f = Frame{SpecVersion: raw.SpecVersion, Type: raw.Type, Headers: headers, Data: raw.Data}
	return nil
}

type Status struct {
	State                            string
	ReconnectAttempt                 int
	LastConnectedAt, LastHeartbeatAt time.Time
}

type response struct {
	Code    int               `json:"code"`
	Headers map[string]string `json:"headers"`
	Message string            `json:"message"`
	Data    string            `json:"data"`
}

type connectionEndpoint struct {
	Endpoint string `json:"endpoint"`
	Ticket   string `json:"ticket"`
}

// Client has one Run lifecycle. onMessage must return promptly after durable
// acceptance; its ACK is diagnostic and does not guarantee robot redelivery.
// Application callbacks run off the read loop so system heartbeats stay live.
type Client struct {
	cfg             Config
	onMessage       func(Frame) bool
	onStatus        func(Status)
	mu              sync.Mutex
	status          Status
	cancel          context.CancelFunc
	started, closed bool
	// Private seams for local protocol tests; no configurable credential endpoint.
	openURL    string
	httpClient *http.Client
	dial       func(context.Context, string) (*websocket.Conn, error)
}

func NewClient(cfg Config, onMessage func(Frame) bool, onStatus func(Status)) *Client {
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = 5 * time.Second
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 120 * time.Second
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
	return &Client{cfg: cfg, onMessage: onMessage, onStatus: onStatus, openURL: OpenURL, httpClient: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
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

// Run obtains a new single-use ticket on every connection attempt. Credentials,
// tickets, raw responses and URL-bearing network errors never leave this package.
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
	if strings.TrimSpace(c.cfg.ClientID) == "" || strings.TrimSpace(c.cfg.ClientSecret) == "" {
		c.publish("auth_failed", 0)
		return ErrConfiguration
	}
	attempt := 0
	for {
		if ctx.Err() != nil {
			c.publish("stopped", 0)
			return ctx.Err()
		}
		c.publish("authenticating", attempt)
		endpoint, err := c.openConnection(ctx)
		connected := false
		if err == nil {
			c.publish("connecting", attempt)
			connected, err = c.runConnection(ctx, endpoint)
		}
		if ctx.Err() != nil {
			c.publish("stopped", 0)
			return ctx.Err()
		}
		if errors.Is(err, ErrAuthFailed) {
			c.publish("auth_failed", 0)
			return ErrAuthFailed
		}
		if connected {
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

func validateEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "wss" || u.Opaque != "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.RawQuery != "" || (!strings.EqualFold(u.Host, "wss-open-connection.dingtalk.com") && !strings.EqualFold(u.Host, "wss-open-connection.dingtalk.com:443")) || u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return nil, ErrEndpoint
	}
	return u, nil
}

func (c *Client) openConnection(ctx context.Context) (string, error) {
	requestBody := struct {
		ClientID      string `json:"clientId"`
		ClientSecret  string `json:"clientSecret"`
		Subscriptions []struct {
			Type  string `json:"type"`
			Topic string `json:"topic"`
		} `json:"subscriptions"`
		UserAgent string `json:"ua"`
	}{ClientID: c.cfg.ClientID, ClientSecret: c.cfg.ClientSecret, UserAgent: "fde-sdk-go/1.0.0"}
	requestBody.Subscriptions = append(requestBody.Subscriptions, struct {
		Type  string `json:"type"`
		Topic string `json:"topic"`
	}{Type: "CALLBACK", Topic: RobotTopic})
	body, _ := json.Marshal(requestBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.openURL, bytes.NewReader(body))
	if err != nil {
		return "", ErrConfiguration
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", ErrDisconnected
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return "", ErrDisconnected
	}
	var endpoint connectionEndpoint
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&endpoint); err != nil {
		return "", ErrProtocol
	}
	if endpoint.Ticket == "" || len(endpoint.Ticket) > 8192 {
		return "", ErrProtocol
	}
	u, err := validateEndpoint(endpoint.Endpoint)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("ticket", endpoint.Ticket)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

type readResult struct {
	frame Frame
	err   error
}

func readFrames(ctx context.Context, conn *websocket.Conn, out chan<- readResult) {
	for {
		typ, data, err := conn.Read(ctx)
		var frame Frame
		if err == nil && (typ != websocket.MessageText || json.Unmarshal(data, &frame) != nil || frame.Headers == nil) {
			err = ErrProtocol
		}
		select {
		case out <- readResult{frame, err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) dialConnection(ctx context.Context, endpoint string) (*websocket.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if c.dial != nil {
		return c.dial(dialCtx, endpoint)
	}
	conn, resp, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, ErrDisconnected
	}
	return conn, nil
}

func (c *Client) writeResponse(ctx context.Context, conn *websocket.Conn, frame Frame, code int, data string) error {
	id := frame.Headers["messageId"]
	if id == "" || len(id) > 1024 {
		return ErrProtocol
	}
	message := "OK"
	if code == 500 {
		message = "internal error"
	}
	if code == 404 {
		message = "topic not found"
	}
	response := response{Code: code, Headers: map[string]string{"messageId": id, "contentType": "application/json"}, Message: message, Data: data}
	body, _ := json.Marshal(response)
	writeCtx, cancel := context.WithTimeout(ctx, c.cfg.AckTimeout)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, body); err != nil {
		return ErrDisconnected
	}
	return nil
}

func (c *Client) accept(frame Frame) (accepted bool) {
	defer func() {
		if recover() != nil {
			accepted = false
		}
	}()
	return c.onMessage != nil && c.onMessage(frame)
}

func (c *Client) runConnection(ctx context.Context, endpoint string) (bool, error) {
	conn, err := c.dialConnection(ctx, endpoint)
	if err != nil {
		return false, ErrDisconnected
	}
	conn.SetReadLimit(1 << 20)
	sessionCtx, cancel := context.WithCancel(ctx)
	defer func() { cancel(); _ = conn.CloseNow() }()
	reads := make(chan readResult, 32)
	go readFrames(sessionCtx, conn, reads)
	callbacks := make(chan Frame, 32)
	type acceptance struct {
		frame    Frame
		accepted bool
	}
	accepted := make(chan acceptance, 32)
	var processed atomic.Int32
	networkClosedSignal := make(chan struct{})
	go func() {
		for {
			select {
			case <-sessionCtx.Done():
				return
			case frame := <-callbacks:
				if sessionCtx.Err() != nil {
					return
				}
				result := acceptance{frame, c.accept(frame)}
				processed.Add(1)
				select {
				case accepted <- result:
				case <-networkClosedSignal:
					// The drain loop still needs to observe the processed count.
					// A full channel already has a result that will wake it.
					select {
					case accepted <- result:
					default:
					}
				case <-sessionCtx.Done():
					return
				}
			}
		}
	}()
	c.publish("connected", 0)
	idle := time.NewTimer(c.cfg.IdleTimeout)
	defer idle.Stop()
	pingResult := make(chan error, 1)
	probing, draining, networkClosed := false, false, false
	queued, acknowledged := 0, 0
	readerFinished := false
	var drainCtx context.Context
	var stopDrain context.CancelFunc
	var drainDone <-chan struct{}
	defer func() {
		if stopDrain != nil {
			stopDrain()
		}
	}()
	resetIdle := func() {
		if draining {
			return
		}
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
		idle.Reset(c.cfg.IdleTimeout)
	}
	startDrain := func(closed bool) {
		if closed && !networkClosed {
			networkClosed = true
			close(networkClosedSignal)
			_ = conn.CloseNow()
		}
		if draining {
			return
		}
		draining = true
		idle.Stop()
		// Official disconnect gives ten silent seconds to complete in-flight work.
		// Preserve already-read callbacks even on network EOF, then get a new ticket.
		drainCtx, stopDrain = context.WithTimeout(sessionCtx, 10*time.Second)
		drainDone = drainCtx.Done()
	}
	writeACK := func(frame Frame, code int, data string) error {
		if networkClosed {
			return nil
		}
		writeCtx := sessionCtx
		if draining {
			writeCtx = drainCtx
		}
		return c.writeResponse(writeCtx, conn, frame, code, data)
	}
	for {
		if draining && ((!networkClosed && acknowledged == queued) || (networkClosed && readerFinished && processed.Load() == int32(queued))) {
			return true, ErrDisconnected
		}
		select {
		case <-sessionCtx.Done():
			return true, ErrDisconnected
		case <-drainDone:
			return true, ErrDisconnected
		case result := <-reads:
			if result.err != nil {
				readerFinished = true
				startDrain(true)
				continue
			}
			resetIdle()
			frame := result.frame
			if frame.SpecVersion != "" && frame.SpecVersion != "1.0" {
				startDrain(true)
				continue
			}
			if frame.Type == "SYSTEM" && frame.Headers["topic"] == "disconnect" {
				startDrain(false)
				continue
			}
			if frame.Headers["messageId"] == "" || len(frame.Headers["messageId"]) > 1024 {
				startDrain(true)
				continue
			}
			if frame.Type == "SYSTEM" && frame.Headers["topic"] == "ping" {
				if err := writeACK(frame, 200, frame.Data); err != nil {
					startDrain(true)
					continue
				}
				if !networkClosed {
					c.publish("heartbeat", 0)
				}
				continue
			}
			if frame.Type == "CALLBACK" && frame.Headers["topic"] == RobotTopic && (frame.Headers["contentType"] == "" || frame.Headers["contentType"] == "application/json") {
				if draining && !networkClosed {
					continue
				}
				if networkClosed {
					select {
					case callbacks <- frame:
						queued++
					case <-sessionCtx.Done():
						return true, ErrDisconnected
					case <-drainDone:
						return true, ErrDisconnected
					}
					continue
				}
				select {
				case callbacks <- frame:
					queued++
				default:
					if err := writeACK(frame, 500, `{"response":null}`); err != nil {
						startDrain(true)
					}
				}
				continue
			}
			if err := writeACK(frame, 404, `{"response":null}`); err != nil {
				startDrain(true)
			}
		case result := <-accepted:
			acknowledged++
			code := 200
			if !result.accepted {
				code = 500
			}
			if err := writeACK(result.frame, code, `{"response":null}`); err != nil {
				startDrain(true)
			}
		case <-idle.C:
			if !probing && !draining {
				probing = true
				go func() {
					pingCtx, stop := context.WithTimeout(sessionCtx, c.cfg.AckTimeout)
					defer stop()
					pingResult <- conn.Ping(pingCtx)
				}()
			}
		case err := <-pingResult:
			probing = false
			if err != nil {
				startDrain(true)
				continue
			}
			c.publish("heartbeat", 0)
			resetIdle()
		}
	}
}
