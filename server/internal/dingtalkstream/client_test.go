package dingtalkstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func testConfig() Config {
	return Config{ClientID: "test-client", ClientSecret: "sensitive-client-secret", AckTimeout: 200 * time.Millisecond, ReconnectMin: 3 * time.Millisecond, ReconnectMax: 10 * time.Millisecond, IdleTimeout: time.Second}
}

func writeFrame(t *testing.T, conn *websocket.Conn, frame Frame) bool {
	t.Helper()
	raw, _ := json.Marshal(frame)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, raw) == nil
}

func readResponse(conn *websocket.Conn) (response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, raw, err := conn.Read(ctx)
	var result response
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}

func callbackFrame(id string) Frame {
	return Frame{SpecVersion: "1.0", Type: "CALLBACK", Headers: map[string]string{"messageId": id, "topic": RobotTopic, "contentType": "application/json", "time": "1690362102194"}, Data: `{"msgId":"m1","text":{"content":"hello"}}`}
}

func pingFrame(id string) Frame {
	return Frame{Type: "SYSTEM", Headers: map[string]string{"messageId": id, "topic": "ping", "contentType": "application/json"}, Data: `{"opaque":"ping-test-opaque"}`}
}

type mockGateway struct {
	opens       atomic.Int32
	connections atomic.Int32
	tickets     chan string
}

func localClient(t *testing.T, cfg Config, onMessage func(Frame) bool, onStatus func(Status), server func(*websocket.Conn, int32), options func(int32) *websocket.AcceptOptions) (*Client, *mockGateway) {
	t.Helper()
	gateway := &mockGateway{tickets: make(chan string, 32)}
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := gateway.connections.Add(1)
		var opts *websocket.AcceptOptions
		if options != nil {
			opts = options(n)
		}
		conn, err := websocket.Accept(w, r, opts)
		if err != nil {
			t.Error("mock WebSocket upgrade failed")
			return
		}
		defer conn.CloseNow()
		server(conn, n)
	}))
	t.Cleanup(ws.Close)
	open := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := gateway.opens.Add(1)
		var body struct {
			ClientID      string                         `json:"clientId"`
			ClientSecret  string                         `json:"clientSecret"`
			UserAgent     string                         `json:"ua"`
			Subscriptions []struct{ Type, Topic string } `json:"subscriptions"`
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" || json.NewDecoder(r.Body).Decode(&body) != nil || body.ClientID != cfg.ClientID || body.ClientSecret != cfg.ClientSecret || body.UserAgent == "" || len(body.Subscriptions) != 1 || body.Subscriptions[0].Type != "CALLBACK" || body.Subscriptions[0].Topic != RobotTopic {
			t.Error("open request differs from official Stream contract")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(connectionEndpoint{Endpoint: "wss://wss-open-connection.dingtalk.com:443/dynamic/tenant/connect", Ticket: fmt.Sprintf("ticket-%d+/=&", n)})
	}))
	t.Cleanup(open.Close)
	c := NewClient(cfg, onMessage, onStatus)
	c.openURL = open.URL
	c.dial = func(ctx context.Context, raw string) (*websocket.Conn, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Path != "/dynamic/tenant/connect" || u.Host != "wss-open-connection.dingtalk.com:443" || u.Query().Get("ticket") == "" || len(u.Query()) != 1 {
			t.Error("dynamic endpoint path or encoded ticket was lost")
		}
		gateway.tickets <- u.Query().Get("ticket")
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ws.URL, "http"), nil)
		return conn, err
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, gateway
}

func runClient(t *testing.T, c *Client) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { defer close(done); done <- c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("transport did not stop")
		}
	})
	return cancel, done
}

func waitValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for mock Stream event")
		var zero T
		return zero
	}
}

func TestOpenSubscribeRobotACKAndServerPing(t *testing.T) {
	acks := make(chan response, 2)
	messages := make(chan Frame, 1)
	heartbeats := make(chan Status, 1)
	c, gateway := localClient(t, testConfig(), func(frame Frame) bool { messages <- frame; return true }, func(status Status) {
		if !status.LastHeartbeatAt.IsZero() {
			select {
			case heartbeats <- status:
			default:
			}
		}
	}, func(conn *websocket.Conn, _ int32) {
		if !writeFrame(t, conn, callbackFrame("callback-1")) {
			return
		}
		ack, err := readResponse(conn)
		if err != nil {
			return
		}
		acks <- ack
		if !writeFrame(t, conn, pingFrame("system-ping-1")) {
			return
		}
		ack, err = readResponse(conn)
		if err != nil {
			return
		}
		acks <- ack
		_, _ = readResponse(conn)
	}, nil)
	cancel, _ := runClient(t, c)
	frame := waitValue(t, messages)
	if frame.Headers["messageId"] != "callback-1" || frame.Data != callbackFrame("callback-1").Data {
		t.Fatal("callback content changed")
	}
	ack := waitValue(t, acks)
	if ack.Code != 200 || ack.Message != "OK" || ack.Headers["messageId"] != "callback-1" || ack.Headers["contentType"] != "application/json" || ack.Data != `{"response":null}` {
		t.Fatalf("invalid robot ACK: %+v", ack)
	}
	pingAck := waitValue(t, acks)
	if pingAck.Code != 200 || pingAck.Headers["messageId"] != "system-ping-1" || pingAck.Data != pingFrame("").Data {
		t.Fatalf("opaque ping was not echoed: %+v", pingAck)
	}
	if waitValue(t, heartbeats).State != "connected" {
		t.Fatal("heartbeat did not preserve connected status")
	}
	if gateway.opens.Load() != 1 {
		t.Fatal("connected transport repeatedly requested tickets")
	}
	cancel()
}

func TestNumericTimeCallbackAndPingPreserveMetadataAndACK(t *testing.T) {
	acks := make(chan response, 2)
	messages := make(chan Frame, 1)
	c, gateway := localClient(t, testConfig(), func(frame Frame) bool { messages <- frame; return true }, nil, func(conn *websocket.Conn, _ int32) {
		for _, fixture := range []string{
			`{"specVersion":"1.0","type":"CALLBACK","headers":{"topic":"/v1.0/im/bot/messages/get","contentType":"application/json","messageId":"numeric-time-callback","time":1690362102194,"sequence":9007199254740993},"data":"{\"msgId\":\"m-numeric\",\"text\":{\"content\":\"hello\"}}"}`,
			`{"type":"SYSTEM","headers":{"topic":"ping","contentType":"application/json","messageId":"numeric-time-ping","time":1690362102195},"data":"{\"opaque\":\"opaque-numeric-time\"}"}`,
		} {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := conn.Write(ctx, websocket.MessageText, []byte(fixture))
			cancel()
			if err != nil {
				return
			}
			ack, err := readResponse(conn)
			if err != nil {
				return
			}
			acks <- ack
		}
		_, _ = readResponse(conn)
	}, nil)
	cancel, _ := runClient(t, c)
	frame := waitValue(t, messages)
	if frame.Headers["time"] != "1690362102194" || frame.Headers["sequence"] != "9007199254740993" {
		t.Fatalf("numeric metadata lost precision: %+v", frame.Headers)
	}
	if ack := waitValue(t, acks); ack.Code != 200 || ack.Headers["messageId"] != "numeric-time-callback" || ack.Headers["contentType"] != "application/json" || ack.Data != `{"response":null}` {
		t.Fatalf("numeric callback ACK: %+v", ack)
	}
	if ack := waitValue(t, acks); ack.Code != 200 || ack.Headers["messageId"] != "numeric-time-ping" || ack.Data != `{"opaque":"opaque-numeric-time"}` {
		t.Fatalf("numeric ping ACK: %+v", ack)
	}
	if gateway.opens.Load() != 1 || gateway.connections.Load() != 1 {
		t.Fatal("valid numeric metadata triggered reconnection")
	}
	cancel()
}

func TestFrameHeaderTypesPreserveRoutingIdentity(t *testing.T) {
	for _, field := range []string{"topic", "messageId", "contentType"} {
		for _, value := range []string{`123`, `true`, `null`, `[]`, `{}`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				headers := map[string]json.RawMessage{"topic": json.RawMessage(`"/v1.0/im/bot/messages/get"`), "messageId": json.RawMessage(`"routing-id"`), "contentType": json.RawMessage(`"application/json"`)}
				headers[field] = json.RawMessage(value)
				data, _ := json.Marshal(map[string]any{"type": "CALLBACK", "headers": headers, "data": "{}"})
				var frame Frame
				if err := json.Unmarshal(data, &frame); !errors.Is(err, ErrProtocol) {
					t.Fatalf("invalid %s type accepted: %v", field, err)
				}
			})
		}
	}
	for _, value := range []string{`true`, `null`, `[]`, `{}`} {
		var frame Frame
		data := `{"type":"CALLBACK","headers":{"topic":"/v1.0/im/bot/messages/get","messageId":"id","time":` + value + `},"data":"{}"}`
		if err := json.Unmarshal([]byte(data), &frame); !errors.Is(err, ErrProtocol) {
			t.Fatalf("invalid metadata type accepted: %s (%v)", value, err)
		}
	}
}

func TestCallbackFailureAndUnknownTopicDiagnosticACK(t *testing.T) {
	acks := make(chan response, 2)
	c, _ := localClient(t, testConfig(), func(Frame) bool { return false }, nil, func(conn *websocket.Conn, _ int32) {
		if !writeFrame(t, conn, callbackFrame("not-accepted")) {
			return
		}
		ack, err := readResponse(conn)
		if err != nil {
			return
		}
		acks <- ack
		unknown := callbackFrame("unknown")
		unknown.Headers["topic"] = "/not/subscribed"
		if !writeFrame(t, conn, unknown) {
			return
		}
		ack, err = readResponse(conn)
		if err != nil {
			return
		}
		acks <- ack
		_, _ = readResponse(conn)
	}, nil)
	cancel, _ := runClient(t, c)
	if ack := waitValue(t, acks); ack.Code != 500 || ack.Headers["messageId"] != "not-accepted" {
		t.Fatalf("acceptance failure ACK: %+v", ack)
	}
	if ack := waitValue(t, acks); ack.Code != 404 || ack.Headers["messageId"] != "unknown" || ack.Headers["contentType"] != "application/json" || ack.Data != `{"response":null}` || !json.Valid([]byte(ack.Data)) {
		t.Fatalf("unknown topic ACK: %+v", ack)
	}
	cancel()
}

func TestBoundedCallbackOverflowReportsFailureWithoutFalseAcceptance(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	acceptedIDs := make(chan string, 33)
	overflow := make(chan response, 1)
	queuedACKs := make(chan response, 33)
	c, gateway := localClient(t, testConfig(), func(frame Frame) bool {
		if frame.Headers["messageId"] == "bounded-0" {
			close(started)
			<-release
		}
		acceptedIDs <- frame.Headers["messageId"]
		return true
	}, nil, func(conn *websocket.Conn, _ int32) {
		if !writeFrame(t, conn, callbackFrame("bounded-0")) {
			return
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			return
		}
		// One callback is processing, 32 are queued, and the final frame must
		// receive an explicit error rather than a successful acceptance ACK.
		for i := 1; i <= 33; i++ {
			if !writeFrame(t, conn, callbackFrame(fmt.Sprintf("bounded-%d", i))) {
				return
			}
		}
		ack, err := readResponse(conn)
		if err != nil {
			return
		}
		overflow <- ack
		for i := 0; i <= 32; i++ {
			ack, err := readResponse(conn)
			if err != nil {
				return
			}
			queuedACKs <- ack
		}
		_, _ = readResponse(conn)
	}, nil)
	cancel, done := runClient(t, c)
	ack := waitValue(t, overflow)
	if ack.Code != 500 || ack.Headers["messageId"] != "bounded-33" || ack.Headers["contentType"] != "application/json" || ack.Data != `{"response":null}` || !json.Valid([]byte(ack.Data)) {
		t.Fatalf("queue overflow was not reported correctly: %+v", ack)
	}
	select {
	case id := <-acceptedIDs:
		t.Fatalf("blocked callback unexpectedly accepted: %s", id)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	for i := 0; i <= 32; i++ {
		expected := fmt.Sprintf("bounded-%d", i)
		if id := waitValue(t, acceptedIDs); id != expected {
			t.Fatalf("accepted callback order = %q, want %q", id, expected)
		}
		if ack := waitValue(t, queuedACKs); ack.Code != 200 || ack.Headers["messageId"] != expected {
			t.Fatalf("accepted callback ACK differs: %+v", ack)
		}
	}
	select {
	case id := <-acceptedIDs:
		t.Fatalf("overflow frame falsely reached application acceptance: %s", id)
	default:
	}
	if gateway.opens.Load() != 1 {
		t.Fatal("overload handling unexpectedly reopened the connection")
	}
	cancel()
	if err := waitValue(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel did not exit callback processing: %v", err)
	}
}

func TestSystemPingDoesNotWaitForApplicationCallback(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	acks := make(chan response, 2)
	c, _ := localClient(t, testConfig(), func(Frame) bool { close(started); <-release; return true }, nil, func(conn *websocket.Conn, _ int32) {
		if !writeFrame(t, conn, callbackFrame("slow")) {
			return
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			return
		}
		if !writeFrame(t, conn, pingFrame("fast-ping")) {
			return
		}
		ack, err := readResponse(conn)
		if err != nil {
			return
		}
		acks <- ack
		ack, err = readResponse(conn)
		if err != nil {
			return
		}
		acks <- ack
		_, _ = readResponse(conn)
	}, nil)
	cancel, _ := runClient(t, c)
	if ack := waitValue(t, acks); ack.Headers["messageId"] != "fast-ping" {
		t.Fatal("system ping was blocked by application work")
	}
	once.Do(func() { close(release) })
	if ack := waitValue(t, acks); ack.Headers["messageId"] != "slow" || ack.Code != 200 {
		t.Fatal("accepted callback was not acknowledged")
	}
	cancel()
}

func TestDisconnectHasNoACKAndReconnectObtainsFreshTicket(t *testing.T) {
	ready := make(chan struct{}, 1)
	var wrongACK atomic.Bool
	c, gateway := localClient(t, testConfig(), func(Frame) bool { return true }, nil, func(conn *websocket.Conn, n int32) {
		if n == 1 {
			if !writeFrame(t, conn, Frame{Type: "SYSTEM", Headers: map[string]string{"topic": "disconnect", "contentType": "application/json"}, Data: `{"reason":"connection is expired"}`}) {
				return
			}
			if _, err := readResponse(conn); err == nil {
				wrongACK.Store(true)
			}
			return
		}
		if !writeFrame(t, conn, pingFrame("after-reconnect")) {
			return
		}
		if ack, err := readResponse(conn); err == nil && ack.Code == 200 {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
		_, _ = readResponse(conn)
	}, nil)
	cancel, _ := runClient(t, c)
	waitValue(t, ready)
	first, second := waitValue(t, gateway.tickets), waitValue(t, gateway.tickets)
	if first == second || gateway.opens.Load() < 2 || wrongACK.Load() {
		t.Fatal("disconnect reused ticket or sent an undocumented ACK")
	}
	cancel()
}

func TestDisconnectDrainsPreviouslyReadCallbacksBeforeReconnect(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	disconnectSent := make(chan struct{})
	accepted := make(chan string, 2)
	acks := make(chan response, 2)
	reconnected := make(chan struct{}, 1)
	c, gateway := localClient(t, testConfig(), func(frame Frame) bool {
		if frame.Headers["messageId"] == "first" {
			close(started)
			<-release
		}
		accepted <- frame.Headers["messageId"]
		return true
	}, nil, func(conn *websocket.Conn, n int32) {
		if n > 1 {
			select {
			case reconnected <- struct{}{}:
			default:
			}
			_, _ = readResponse(conn)
			return
		}
		if !writeFrame(t, conn, callbackFrame("first")) {
			return
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			return
		}
		if !writeFrame(t, conn, callbackFrame("second")) {
			return
		}
		if !writeFrame(t, conn, Frame{Type: "SYSTEM", Headers: map[string]string{"topic": "disconnect"}, Data: `{"reason":"connection is expired"}`}) {
			return
		}
		close(disconnectSent)
		for i := 0; i < 2; i++ {
			ack, err := readResponse(conn)
			if err != nil {
				return
			}
			acks <- ack
		}
		_, _ = readResponse(conn)
	}, nil)
	cancel, _ := runClient(t, c)
	waitValue(t, disconnectSent)
	if gateway.opens.Load() != 1 {
		t.Fatal("connection replaced before callback persistence")
	}
	releaseOnce.Do(func() { close(release) })
	if first, second := waitValue(t, accepted), waitValue(t, accepted); first != "first" || second != "second" {
		t.Fatalf("callback drain order = %q %q", first, second)
	}
	for i, expected := range []string{"first", "second"} {
		if ack := waitValue(t, acks); ack.Code != 200 || ack.Headers["messageId"] != expected {
			t.Fatalf("drained ACK %d: %+v", i, ack)
		}
	}
	waitValue(t, reconnected)
	cancel()
}

func TestNetworkEOFStillPersistsAlreadyReadCallbacks(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	connectionClosed := make(chan struct{})
	accepted := make(chan string, 2)
	reconnected := make(chan struct{}, 1)
	c, _ := localClient(t, testConfig(), func(frame Frame) bool {
		if frame.Headers["messageId"] == "first" {
			close(started)
			<-release
		}
		accepted <- frame.Headers["messageId"]
		return true
	}, nil, func(conn *websocket.Conn, n int32) {
		if n > 1 {
			select {
			case reconnected <- struct{}{}:
			default:
			}
			_, _ = readResponse(conn)
			return
		}
		if !writeFrame(t, conn, callbackFrame("first")) {
			return
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			return
		}
		if !writeFrame(t, conn, callbackFrame("second")) {
			return
		}
		// The ping reply fences the second callback in the client's FIFO read loop
		// while the application is still blocked on persisting the first one.
		if !writeFrame(t, conn, pingFrame("before-eof")) {
			return
		}
		if ack, err := readResponse(conn); err != nil || ack.Headers["messageId"] != "before-eof" {
			t.Errorf("close fence ACK: %q, %v", ack.Headers["messageId"], err)
			return
		}
		_ = conn.Close(websocket.StatusGoingAway, "rotation")
		close(connectionClosed)
	}, nil)
	cancel, _ := runClient(t, c)
	waitValue(t, connectionClosed)
	releaseOnce.Do(func() { close(release) })
	if first, second := waitValue(t, accepted), waitValue(t, accepted); first != "first" || second != "second" {
		t.Fatalf("EOF lost callback: %q %q", first, second)
	}
	waitValue(t, reconnected)
	cancel()
}

func TestIdleControlPingAndMissingPongReconnect(t *testing.T) {
	cfg := testConfig()
	cfg.IdleTimeout = 15 * time.Millisecond
	cfg.AckTimeout = 40 * time.Millisecond
	heartbeats := make(chan Status, 1)
	c, gateway := localClient(t, cfg, nil, func(status Status) {
		if !status.LastHeartbeatAt.IsZero() {
			select {
			case heartbeats <- status:
			default:
			}
		}
	}, func(conn *websocket.Conn, _ int32) { _, _ = readResponse(conn) }, func(n int32) *websocket.AcceptOptions {
		return &websocket.AcceptOptions{OnPingReceived: func(context.Context, []byte) bool { return n > 1 }}
	})
	cancel, _ := runClient(t, c)
	waitValue(t, heartbeats)
	if gateway.opens.Load() < 2 {
		t.Fatal("idle missing pong did not reconnect with a new ticket")
	}
	cancel()
}

func TestAuthenticationFailureStopsAndDoesNotLeakResponse(t *testing.T) {
	for _, status := range []int{400, 401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"sensitive-client-secret ticket-secret"}`))
			}))
			defer srv.Close()
			c := NewClient(testConfig(), nil, nil)
			c.openURL = srv.URL
			err := c.Run(context.Background())
			if !errors.Is(err, ErrAuthFailed) || calls.Load() != 1 || strings.Contains(err.Error(), "sensitive-client-secret") || strings.Contains(err.Error(), "ticket-secret") {
				t.Fatalf("credential failure behavior: err=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestCloseCancelsBackoffAndInFlightOpen(t *testing.T) {
	for _, block := range []bool{false, true} {
		t.Run(fmt.Sprint(block), func(t *testing.T) {
			started := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				select {
				case started <- struct{}{}:
				default:
				}
				if block {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(503)
			}))
			defer srv.Close()
			cfg := testConfig()
			cfg.ReconnectMin = 10 * time.Second
			cfg.ReconnectMax = 10 * time.Second
			retrying := make(chan struct{}, 1)
			c := NewClient(cfg, nil, func(status Status) {
				if status.State == "reconnecting" {
					select {
					case retrying <- struct{}{}:
					default:
					}
				}
			})
			c.openURL = srv.URL
			_, done := runClient(t, c)
			waitValue(t, started)
			if !block {
				waitValue(t, retrying)
			}
			_ = c.Close()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Close returned %v", err)
				}

			case <-time.After(time.Second):
				t.Fatal("Close did not cancel promptly")
			}
		})
	}
}

func TestEndpointValidationAndOpenRedirectDoNotLeakCredentials(t *testing.T) {
	for _, endpoint := range []string{"wss://wss-open-connection.dingtalk.com/connect", "wss://wss-open-connection.dingtalk.com:443/any/dynamic/path"} {
		if _, err := validateEndpoint(endpoint); err != nil {
			t.Fatalf("official endpoint rejected: %v", err)
		}
	}
	for _, endpoint := range []string{"ws://wss-open-connection.dingtalk.com/connect", "wss://evil.test/connect", "wss://wss-open-connection.dingtalk.com.evil.test/connect", "wss://user@wss-open-connection.dingtalk.com/connect", "wss://wss-open-connection.dingtalk.com:8443/connect", "wss://wss-open-connection.dingtalk.com:/connect", "wss://127.0.0.1/connect", "wss://wss-open-connection.dingtalk.com/connect?ticket=x", "wss://wss-open-connection.dingtalk.com/connect#fragment"} {
		if _, err := validateEndpoint(endpoint); !errors.Is(err, ErrEndpoint) {
			t.Fatalf("unsafe endpoint allowed: %s", endpoint)
		}
	}
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Store(true) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	c := NewClient(testConfig(), nil, nil)
	c.openURL = srv.URL
	if _, err := c.openConnection(context.Background()); err == nil || redirected.Load() {
		t.Fatal("credential-bearing open request followed redirect")
	}
}

func TestMalformedAndOversizedFramesReconnectBeforeAccepting(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(fmt.Sprint(oversized), func(t *testing.T) {
			ready := make(chan struct{}, 1)
			var accepted atomic.Int32
			c, gateway := localClient(t, testConfig(), func(Frame) bool { accepted.Add(1); return true }, nil, func(conn *websocket.Conn, n int32) {
				if n == 1 {
					raw := `{"type":"CALLBACK","headers":null,"data":"malformed"}`
					if oversized {
						raw = strings.Repeat("x", (1<<20)+1)
					}
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_ = conn.Write(ctx, websocket.MessageText, []byte(raw))
					_, _ = readResponse(conn)
					return
				}
				if !writeFrame(t, conn, pingFrame("recovered")) {
					return
				}
				if _, err := readResponse(conn); err == nil {
					select {
					case ready <- struct{}{}:
					default:
					}
				}
				_, _ = readResponse(conn)
			}, nil)
			cancel, _ := runClient(t, c)
			waitValue(t, ready)
			if accepted.Load() != 0 || gateway.opens.Load() < 2 {
				t.Fatal("invalid input reached application callback")
			}
			cancel()
		})
	}
}
