package wecomws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func testConfig() Config {
	return Config{BotID: "test-bot", Secret: "sensitive-bot-secret", HeartbeatInterval: time.Second, AckTimeout: 300 * time.Millisecond, ReconnectMin: 3 * time.Millisecond, ReconnectMax: 10 * time.Millisecond}
}

func readTestFrame(conn *websocket.Conn) (Frame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, raw, err := conn.Read(ctx)
	var frame Frame
	if err == nil {
		err = json.Unmarshal(raw, &frame)
	}
	return frame, err
}

func writeTestFrame(conn *websocket.Conn, frame Frame) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, raw)
}

func ackTestFrame(conn *websocket.Conn, frame Frame) error {
	zero := 0
	return writeTestFrame(conn, Frame{Headers: frame.Headers, ErrCode: &zero})
}

func authenticateTestServer(t *testing.T, conn *websocket.Conn) bool {
	t.Helper()
	frame, err := readTestFrame(conn)
	if err != nil {
		return false
	}
	var body map[string]string
	if err := json.Unmarshal(frame.Body, &body); err != nil || frame.Cmd != "aibot_subscribe" || !strings.HasPrefix(frame.Headers.ReqID, "aibot_subscribe_") || body["bot_id"] != "test-bot" || body["secret"] != "sensitive-bot-secret" {
		t.Error("authentication wire format does not match the official SDK")
		return false
	}
	return ackTestFrame(conn, frame) == nil
}

func testCallback(reqID string) Frame {
	return Frame{Cmd: "aibot_msg_callback", Headers: Headers{ReqID: reqID}, Body: json.RawMessage(`{"msgid":"message-1","msgtype":"text","text":{"content":"你好"},"from":{"userid":"alice"},"chattype":"single"}`)}
}

func localClient(t *testing.T, cfg Config, server func(*websocket.Conn), onMessage func(Frame), onStatus func(Status)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error("mock WebSocket upgrade failed")
			return
		}
		defer conn.CloseNow()
		server(conn)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(cfg, onMessage, onStatus)
	c.dial = func(ctx context.Context) (*websocket.Conn, error) {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		return conn, err
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func waitValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for mock WebSocket event")
		var zero T
		return zero
	}
}

func runTestClient(t *testing.T, c *Client) (context.Context, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	t.Cleanup(cancel)
	return ctx, cancel, done
}

func TestSubscribeCallbackAndSerializedReplyACK(t *testing.T) {
	messages := make(chan Frame, 1)
	sent := make(chan Frame, 4)
	release := make(chan struct{})
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		if !authenticateTestServer(t, conn) || writeTestFrame(conn, testCallback("callback-1")) != nil {
			return
		}
		first := true
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd == "aibot_respond_msg" {
				sent <- frame
				if first {
					first = false
					go func() { <-release; _ = ackTestFrame(conn, frame) }()
					continue
				}
			}
			if ackTestFrame(conn, frame) != nil {
				return
			}
		}
	}, func(f Frame) { messages <- f }, nil)
	ctx, cancel, done := runTestClient(t, c)
	message := waitValue(t, messages)
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- c.ReplyStream(ctx, message.Headers.ReqID, "stream-1", "正在处理", false) }()
	frame := waitValue(t, sent)
	if frame.Headers.ReqID != "callback-1" {
		t.Fatal("reply did not preserve callback req_id")
	}
	go func() { second <- c.ReplyStream(ctx, message.Headers.ReqID, "stream-1", "完成", true) }()
	select {
	case <-sent:
		t.Fatal("second frame sent before the first acknowledgement")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := waitValue(t, first); err != nil {
		t.Fatal(err)
	}
	final := waitValue(t, sent)
	var body struct {
		Type   string `json:"msgtype"`
		Stream struct {
			ID      string `json:"id"`
			Finish  bool   `json:"finish"`
			Content string `json:"content"`
		} `json:"stream"`
	}
	if json.Unmarshal(final.Body, &body) != nil || body.Type != "stream" || body.Stream.ID != "stream-1" || !body.Stream.Finish || body.Stream.Content != "完成" {
		t.Fatal("stream reply body does not match the official SDK")
	}
	if err := waitValue(t, second); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := waitValue(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run cancellation error = %v", err)
	}
}

func TestMessageCallbackMayWaitForACKWithoutBlockingRead(t *testing.T) {
	result := make(chan error, 1)
	var c *Client
	c = localClient(t, testConfig(), func(conn *websocket.Conn) {
		if !authenticateTestServer(t, conn) || writeTestFrame(conn, testCallback("reply-inside-callback")) != nil {
			return
		}
		for {
			frame, err := readTestFrame(conn)
			if err != nil || ackTestFrame(conn, frame) != nil {
				return
			}
		}
	}, func(frame Frame) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result <- c.ReplyStream(ctx, frame.Headers.ReqID, "s1", "完成", true)
	}, nil)
	_, cancel, done := runTestClient(t, c)
	if err := waitValue(t, result); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitValue(t, done)
}

func TestHeartbeatMissesReconnectAndSuccessfulACKUpdatesStatus(t *testing.T) {
	cfg := testConfig()
	cfg.HeartbeatInterval, cfg.AckTimeout = 15*time.Millisecond, 20*time.Millisecond
	var connections atomic.Int32
	heartbeats := make(chan Status, 4)
	c := localClient(t, cfg, func(conn *websocket.Conn) {
		n := connections.Add(1)
		if !authenticateTestServer(t, conn) {
			return
		}
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd != "ping" {
				t.Error("unexpected frame during heartbeat test")
			}
			if n > 1 && ackTestFrame(conn, frame) != nil {
				return
			}
		}
	}, nil, func(s Status) {
		if !s.LastHeartbeatAt.IsZero() {
			select {
			case heartbeats <- s:
			default:
			}
		}
	})
	_, cancel, done := runTestClient(t, c)
	status := waitValue(t, heartbeats)
	if connections.Load() < 2 || status.State != "connected" || status.LastConnectedAt.IsZero() {
		t.Fatal("missing heartbeat acknowledgement did not reconnect and recover")
	}
	cancel()
	waitValue(t, done)
}

func TestAuthenticationFailureIsTerminalAndRedacted(t *testing.T) {
	var connections atomic.Int32
	statuses := make(chan Status, 10)
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		connections.Add(1)
		frame, err := readTestFrame(conn)
		if err != nil {
			return
		}
		code := 400
		_ = writeTestFrame(conn, Frame{Headers: frame.Headers, ErrCode: &code, ErrMsg: "sensitive-bot-secret full-frame-could-be-here"})
		_, _ = readTestFrame(conn)
	}, nil, func(s Status) { statuses <- s })
	_, _, done := runTestClient(t, c)
	err := waitValue(t, done)
	if !errors.Is(err, ErrAuthFailed) || strings.Contains(err.Error(), "sensitive-bot-secret") {
		t.Fatalf("authentication failure was not terminal/redacted: %v", err)
	}
	if connections.Load() != 1 {
		t.Fatal("authentication rejection triggered automatic reconnect")
	}
	var last Status
	for len(statuses) > 0 {
		last = <-statuses
	}
	if last.State != "auth_failed" || strings.Contains(fmt.Sprint(last), "sensitive-bot-secret") {
		t.Fatal("status did not report a safe terminal authentication failure")
	}
}

func TestAuthenticationRequiresMatchingExplicitZeroACK(t *testing.T) {
	cfg := testConfig()
	cfg.AckTimeout = 25 * time.Millisecond
	var connections atomic.Int32
	messages := make(chan Frame, 1)
	c := localClient(t, cfg, func(conn *websocket.Conn) {
		n := connections.Add(1)
		if n == 1 {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			zero := 0
			_ = writeTestFrame(conn, Frame{Headers: Headers{ReqID: "wrong-auth-id"}, ErrCode: &zero})
			_ = writeTestFrame(conn, Frame{Headers: frame.Headers})
			_, _ = readTestFrame(conn)
			return
		}
		if !authenticateTestServer(t, conn) {
			return
		}
		_ = writeTestFrame(conn, testCallback("authenticated"))
		for {
			if _, err := readTestFrame(conn); err != nil {
				return
			}
		}
	}, func(f Frame) { messages <- f }, nil)
	_, cancel, done := runTestClient(t, c)
	if frame := waitValue(t, messages); frame.Headers.ReqID != "authenticated" || connections.Load() < 2 {
		t.Fatal("unrelated or missing errcode authenticated the connection")
	}
	cancel()
	waitValue(t, done)
}

func TestDisconnectedEventStopsWithoutCompetingReconnect(t *testing.T) {
	var connections atomic.Int32
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		connections.Add(1)
		if !authenticateTestServer(t, conn) {
			return
		}
		_ = writeTestFrame(conn, Frame{Cmd: "aibot_event_callback", Headers: Headers{ReqID: "disconnect"}, Body: json.RawMessage(`{"event":{"eventtype":"disconnected_event"}}`)})
		_, _ = readTestFrame(conn)
	}, nil, nil)
	_, _, done := runTestClient(t, c)
	if err := waitValue(t, done); !errors.Is(err, ErrSuperseded) || connections.Load() != 1 {
		t.Fatalf("superseded connection competed to reconnect: err=%v attempts=%d", err, connections.Load())
	}
}

func TestOldConnectionRequestCannotReplyAfterReconnect(t *testing.T) {
	var connections atomic.Int32
	closeFirst := make(chan struct{})
	messages := make(chan Frame, 3)
	replies := make(chan string, 3)
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		n := connections.Add(1)
		if !authenticateTestServer(t, conn) {
			return
		}
		if n == 1 {
			_ = writeTestFrame(conn, testCallback("old-req"))
			<-closeFirst
			return
		}
		_ = writeTestFrame(conn, testCallback("new-req"))
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd == "aibot_respond_msg" {
				replies <- frame.Headers.ReqID
			}
			if ackTestFrame(conn, frame) != nil {
				return
			}
		}
	}, func(frame Frame) { messages <- frame }, nil)
	ctx, cancel, done := runTestClient(t, c)
	if f := waitValue(t, messages); f.Headers.ReqID != "old-req" {
		t.Fatal("missing first connection message")
	}
	close(closeFirst)
	if f := waitValue(t, messages); f.Headers.ReqID != "new-req" {
		t.Fatal("missing replacement connection message")
	}
	if err := c.ReplyStream(ctx, "old-req", "old-stream", "late answer", true); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("old request was not rejected: %v", err)
	}
	if err := c.ReplyStream(ctx, "new-req", "new-stream", "new answer", true); err != nil {
		t.Fatal(err)
	}
	if reqID := waitValue(t, replies); reqID != "new-req" {
		t.Fatal("old AI answer escaped onto the replacement connection")
	}
	cancel()
	waitValue(t, done)
}

func TestReplyACKTimeoutDoesNotSendQueuedOrReplayUncertainWrites(t *testing.T) {
	cfg := testConfig()
	cfg.AckTimeout = 40 * time.Millisecond
	var connections, writes atomic.Int32
	messages := make(chan Frame, 1)
	firstWritten := make(chan struct{}, 1)
	c := localClient(t, cfg, func(conn *websocket.Conn) {
		n := connections.Add(1)
		if !authenticateTestServer(t, conn) {
			return
		}
		if n == 1 {
			_ = writeTestFrame(conn, testCallback("uncertain-req"))
		}
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd == "aibot_respond_msg" {
				writes.Add(1)
				select {
				case firstWritten <- struct{}{}:
				default:
				}
			}
		}
	}, func(frame Frame) { messages <- frame }, nil)
	ctx, cancel, done := runTestClient(t, c)
	waitValue(t, messages)
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- c.ReplyStream(ctx, "uncertain-req", "s", "intermediate", false) }()
	waitValue(t, firstWritten)
	go func() { second <- c.ReplyStream(ctx, "uncertain-req", "s", "final", true) }()
	if waitValue(t, first) == nil || waitValue(t, second) == nil {
		t.Fatal("missing ACK reported success")
	}
	deadline := time.Now().Add(time.Second)
	for connections.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if connections.Load() < 2 || writes.Load() != 1 {
		t.Fatalf("uncertain write replayed or next frame sent: connections=%d writes=%d", connections.Load(), writes.Load())
	}
	cancel()
	waitValue(t, done)
}

func TestCloseInterruptsPendingReplyAndReconnectBackoff(t *testing.T) {
	messages := make(chan Frame, 1)
	written := make(chan struct{}, 1)
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		if !authenticateTestServer(t, conn) {
			return
		}
		_ = writeTestFrame(conn, testCallback("close-req"))
		for {
			_, err := readTestFrame(conn)
			if err != nil {
				return
			}
			select {
			case written <- struct{}{}:
			default:
			}
		}
	}, func(f Frame) { messages <- f }, nil)
	ctx, _, done := runTestClient(t, c)
	waitValue(t, messages)
	reply := make(chan error, 1)
	go func() { reply <- c.ReplyStream(ctx, "close-req", "s", "answer", true) }()
	waitValue(t, written)
	started := time.Now()
	_ = c.Close()
	waitValue(t, done)
	if waitValue(t, reply) == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatal("Close did not promptly release pending reply and Run")
	}

	backoff := NewClient(Config{BotID: "b", Secret: "s", ReconnectMin: time.Hour, ReconnectMax: time.Hour}, nil, nil)
	ready := make(chan struct{}, 1)
	backoff.dial = func(context.Context) (*websocket.Conn, error) { ready <- struct{}{}; return nil, ErrDisconnected }
	_, _, stopped := runTestClient(t, backoff)
	waitValue(t, ready)
	_ = backoff.Close()
	if err := waitValue(t, stopped); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestEndpointRestrictionAndReplyValidation(t *testing.T) {
	for _, target := range []string{"ws://openws.work.weixin.qq.com", "wss://openws.work.weixin.qq.com.evil.test", "wss://user:secret@openws.work.weixin.qq.com", "wss://127.0.0.1", DefaultURL + "?secret=x"} {
		c := NewClient(Config{BotID: "test-bot", Secret: "sensitive-bot-secret", URL: target}, nil, nil)
		if err := c.Run(context.Background()); !errors.Is(err, ErrConfiguration) || strings.Contains(err.Error(), "sensitive-bot-secret") {
			t.Fatal("unsafe public endpoint was accepted or configuration error leaked credentials")
		}
	}
	c := NewClient(testConfig(), nil, nil)
	for _, args := range [][3]string{{"", "stream", "a"}, {"req", "", "a"}, {"req", "stream", strings.Repeat("a", 20481)}, {"req", "stream", string([]byte{0xff})}} {
		if err := c.ReplyStream(context.Background(), args[0], args[1], args[2], true); !errors.Is(err, ErrInvalidReply) {
			t.Fatal("invalid reply accepted")
		}
	}
}

func TestOversizedInboundFrameReconnects(t *testing.T) {
	var connections atomic.Int32
	recovered := make(chan Frame, 1)
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		n := connections.Add(1)
		if !authenticateTestServer(t, conn) {
			return
		}
		if n == 1 {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = conn.Write(ctx, websocket.MessageText, []byte(strings.Repeat("x", (1<<20)+1)))
		} else {
			_ = writeTestFrame(conn, testCallback("recovered"))
		}
		for {
			if _, err := readTestFrame(conn); err != nil {
				return
			}
		}
	}, func(frame Frame) { recovered <- frame }, nil)
	_, cancel, done := runTestClient(t, c)
	if frame := waitValue(t, recovered); frame.Headers.ReqID != "recovered" || connections.Load() < 2 {
		t.Fatal("oversized inbound frame was not rejected and recovered")
	}
	cancel()
	waitValue(t, done)
}

func queuedTestStream(t *testing.T, ctx context.Context, reqID, streamID string, finish bool) replyRequest {
	t.Helper()
	body, err := json.Marshal(map[string]any{"msgtype": "stream", "stream": map[string]any{"id": streamID, "content": "queued", "finish": finish}})
	if err != nil {
		t.Fatal(err)
	}
	return replyRequest{ctx: ctx, frame: Frame{Cmd: "aibot_respond_msg", Headers: Headers{ReqID: reqID}, Body: body}, streamID: streamID, finish: finish, result: make(chan error, 1)}
}

func activeTestSession(t *testing.T, c *Client) *session {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		t.Fatal("missing authenticated session")
	}
	return c.active
}

func TestStreamFinalACKRejectsQueuedAndLaterUpdates(t *testing.T) {
	messages := make(chan Frame, 2)
	written := make(chan Frame, 4)
	releaseFinal := make(chan struct{})
	var writes atomic.Int32
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		if !authenticateTestServer(t, conn) {
			return
		}
		_ = writeTestFrame(conn, testCallback("final-req"))
		_ = writeTestFrame(conn, testCallback("barrier-req"))
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd != "aibot_respond_msg" {
				_ = ackTestFrame(conn, frame)
				continue
			}
			writes.Add(1)
			written <- frame
			if frame.Headers.ReqID == "final-req" {
				go func() { <-releaseFinal; _ = ackTestFrame(conn, frame) }()
			} else {
				_ = ackTestFrame(conn, frame)
			}
		}
	}, func(frame Frame) { messages <- frame }, nil)
	ctx, cancel, done := runTestClient(t, c)
	waitValue(t, messages)
	waitValue(t, messages)
	final := make(chan error, 1)
	go func() { final <- c.ReplyStream(ctx, "final-req", "finished-stream", "final", true) }()
	waitValue(t, written)
	queued := queuedTestStream(t, ctx, "final-req", "finished-stream", false)
	activeTestSession(t, c).outbox <- queued
	// FIFO outbox plus a separate callback's ACK proves the update above was
	// queued behind the final before its ACK is released.
	if err := c.ReplyStream(ctx, "barrier-req", "barrier-stream", "barrier", true); err != nil {
		t.Fatal(err)
	}
	waitValue(t, written)
	close(releaseFinal)
	if err := waitValue(t, final); err != nil {
		t.Fatal(err)
	}
	if err := waitValue(t, queued.result); !errors.Is(err, ErrStreamFinished) {
		t.Fatalf("queued after final = %v", err)
	}
	if err := c.ReplyStream(ctx, "final-req", "finished-stream", "late final", true); !errors.Is(err, ErrStreamFinished) {
		t.Fatalf("later update = %v", err)
	}
	if writes.Load() != 2 {
		t.Fatalf("finished stream wrote again: %d frames", writes.Load())
	}
	cancel()
	waitValue(t, done)
}

func TestStreamAndCallbackUseSeparateReplyWindows(t *testing.T) {
	messages := make(chan Frame, 1)
	var writes atomic.Int32
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		if !authenticateTestServer(t, conn) || writeTestFrame(conn, testCallback("old-callback")) != nil {
			return
		}
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd == "aibot_respond_msg" {
				writes.Add(1)
			}
			if ackTestFrame(conn, frame) != nil {
				return
			}
		}
	}, func(frame Frame) { messages <- frame }, nil)
	ctx, cancel, done := runTestClient(t, c)
	waitValue(t, messages)
	s := activeTestSession(t, c)
	s.knownMu.Lock()
	s.known["old-callback"] = time.Now().Add(-23 * time.Hour)
	s.knownMu.Unlock()
	if err := c.ReplyStream(ctx, "old-callback", "fresh-stream", "start now", false); err != nil {
		t.Fatalf("valid 23-hour callback rejected: %v", err)
	}
	c.mu.Lock()
	state := c.streams["fresh-stream"]
	c.mu.Unlock()
	if time.Since(state.startedAt) > time.Second {
		t.Fatal("stream clock started at callback receipt")
	}
	state.startedAt = time.Now().Add(-streamReplyWindow + time.Minute)
	expectedStart := state.startedAt
	c.mu.Lock()
	c.streams["fresh-stream"] = state
	c.mu.Unlock()
	if err := c.ReplyStream(ctx, "old-callback", "fresh-stream", "refresh", false); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	unchanged := c.streams["fresh-stream"].startedAt
	state.startedAt = time.Now().Add(-streamReplyWindow)
	c.streams["fresh-stream"] = state
	c.mu.Unlock()
	if !unchanged.Equal(expectedStart) {
		t.Fatal("refresh reset first-write clock")
	}
	if err := c.ReplyStream(ctx, "old-callback", "fresh-stream", "too late", true); !errors.Is(err, ErrStreamExpired) {
		t.Fatalf("expired stream = %v", err)
	}
	s.knownMu.Lock()
	s.known["old-callback"] = time.Now().Add(-callbackReplyWindow)
	s.knownMu.Unlock()
	if err := c.ReplyStream(ctx, "old-callback", "another-stream", "late callback", true); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("expired callback = %v", err)
	}
	if writes.Load() != 2 {
		t.Fatalf("expired frame escaped: %d writes", writes.Load())
	}
	cancel()
	waitValue(t, done)
}

func TestQueuedStreamExpiryAndCallerCancellationDoNotWrite(t *testing.T) {
	for _, mode := range []string{"stream_expired", "caller_canceled"} {
		t.Run(mode, func(t *testing.T) {
			messages := make(chan Frame, 1)
			written := make(chan struct{}, 1)
			release := make(chan struct{})
			var writes atomic.Int32
			c := localClient(t, testConfig(), func(conn *websocket.Conn) {
				if !authenticateTestServer(t, conn) || writeTestFrame(conn, testCallback("queued-req")) != nil {
					return
				}
				for {
					frame, err := readTestFrame(conn)
					if err != nil {
						return
					}
					if frame.Cmd == "aibot_respond_msg" {
						writes.Add(1)
						written <- struct{}{}
						go func() { <-release; _ = ackTestFrame(conn, frame) }()
					} else {
						_ = ackTestFrame(conn, frame)
					}
				}
			}, func(frame Frame) { messages <- frame }, nil)
			ctx, cancel, done := runTestClient(t, c)
			waitValue(t, messages)
			first := make(chan error, 1)
			go func() { first <- c.ReplyStream(ctx, "queued-req", "queued-stream", "initial", false) }()
			waitValue(t, written)
			caller, cancelCaller := context.WithCancel(ctx)
			defer cancelCaller()
			queued := queuedTestStream(t, caller, "queued-req", "queued-stream", true)
			activeTestSession(t, c).outbox <- queued
			want := ErrStreamExpired
			if mode == "caller_canceled" {
				cancelCaller()
				want = context.Canceled
			} else {
				c.mu.Lock()
				state := c.streams["queued-stream"]
				state.startedAt = time.Now().Add(-streamReplyWindow)
				c.streams["queued-stream"] = state
				c.mu.Unlock()
			}
			close(release)
			if err := waitValue(t, first); err != nil {
				t.Fatal(err)
			}
			if err := waitValue(t, queued.result); !errors.Is(err, want) {
				t.Fatalf("queued rejection = %v, want %v", err, want)
			}
			if writes.Load() != 1 {
				t.Fatalf("rejected queued reply wrote late: %d", writes.Load())
			}
			cancel()
			waitValue(t, done)
		})
	}
}

func TestStreamBindingAndRejectedFinalACK(t *testing.T) {
	messages := make(chan Frame, 2)
	var writes atomic.Int32
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		if !authenticateTestServer(t, conn) {
			return
		}
		_ = writeTestFrame(conn, testCallback("owner"))
		_ = writeTestFrame(conn, testCallback("other"))
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd == "aibot_respond_msg" && writes.Add(1) == 1 {
				code := 9
				_ = writeTestFrame(conn, Frame{Headers: frame.Headers, ErrCode: &code})
			} else {
				_ = ackTestFrame(conn, frame)
			}
		}
	}, func(frame Frame) { messages <- frame }, nil)
	ctx, cancel, done := runTestClient(t, c)
	waitValue(t, messages)
	waitValue(t, messages)
	if err := c.ReplyStream(ctx, "owner", "bound-stream", "rejected final", true); !errors.Is(err, ErrRejected) {
		t.Fatalf("rejected final = %v", err)
	}
	if err := c.ReplyStream(ctx, "other", "bound-stream", "wrong callback", false); !errors.Is(err, ErrStreamBinding) {
		t.Fatalf("cross-callback stream = %v", err)
	}
	if err := c.ReplyStream(ctx, "owner", "bound-stream", "valid final", true); err != nil {
		t.Fatalf("failed ACK incorrectly finished stream: %v", err)
	}
	if err := c.ReplyStream(ctx, "owner", "bound-stream", "late", false); !errors.Is(err, ErrStreamFinished) {
		t.Fatalf("successful final not recorded: %v", err)
	}
	if writes.Load() != 2 {
		t.Fatalf("wrong stream emitted frame: %d", writes.Load())
	}
	cancel()
	waitValue(t, done)
}

func TestRepeatedCallbackOnNewConnectionCannotReplayOldStream(t *testing.T) {
	var connections atomic.Int32
	messages := make(chan Frame, 2)
	closeFirst := make(chan struct{})
	writes := make(chan string, 2)
	c := localClient(t, testConfig(), func(conn *websocket.Conn) {
		n := connections.Add(1)
		if !authenticateTestServer(t, conn) || writeTestFrame(conn, testCallback("same-callback")) != nil {
			return
		}
		for {
			frame, err := readTestFrame(conn)
			if err != nil {
				return
			}
			if frame.Cmd == "aibot_respond_msg" {
				writes <- string(frame.Body)
			}
			if ackTestFrame(conn, frame) != nil {
				return
			}
			if n == 1 && frame.Cmd == "aibot_respond_msg" {
				<-closeFirst
				return
			}
		}
	}, func(frame Frame) { messages <- frame }, nil)
	ctx, cancel, done := runTestClient(t, c)
	waitValue(t, messages)
	if err := c.ReplyStream(ctx, "same-callback", "old-stream", "initial", false); err != nil {
		t.Fatal(err)
	}
	waitValue(t, writes)
	close(closeFirst)
	waitValue(t, messages)
	if err := c.ReplyStream(ctx, "same-callback", "old-stream", "late old answer", true); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("old stream moved across connection generation: %v", err)
	}
	if err := c.ReplyStream(ctx, "same-callback", "new-stream", "explicit new stream", true); err != nil {
		t.Fatal(err)
	}
	if body := waitValue(t, writes); !strings.Contains(body, "new-stream") {
		t.Fatal("old stream escaped onto replacement connection")
	}
	cancel()
	waitValue(t, done)
}
