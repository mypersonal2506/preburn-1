package cache_test

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/logging"
)

type replyDroppingServer struct {
	url         *url.URL
	scriptsSent atomic.Int64
}

func TestRunScriptRecoversAfterScriptFlush(t *testing.T) {
	// Not parallel: SCRIPT FLUSH empties the shared script cache, and a flush
	// between another test's reload and its retry would fail that test.
	client := cachetest.NewClient(t)
	increment := cache.NewScript("increment", "return redis.call('INCR', KEYS[1])")
	counter := client.Key("script_counter")

	if client.ScriptsLoaded() {
		t.Error("ScriptsLoaded = true before LoadScripts")
	}
	if err := client.LoadScripts(t.Context(), increment); err != nil {
		t.Fatalf("load scripts: %v", err)
	}
	if !client.ScriptsLoaded() {
		t.Error("ScriptsLoaded = false after LoadScripts")
	}
	assertScriptResult(t, client, increment, []string{counter}, int64(1))

	if err := client.Redis().ScriptFlush(t.Context()).Err(); err != nil {
		t.Fatalf("script flush: %v", err)
	}

	assertScriptResult(t, client, increment, []string{counter}, int64(2))
}

func TestRunScriptNeverResendsAfterALostReply(t *testing.T) {
	t.Parallel()
	server := newReplyDroppingServer(t)
	client, err := cache.Open(t.Context(), config.Config{RedisURL: server.url, RedisKeyPrefix: uniquePrefix()}, logging.New(io.Discard, slog.LevelDebug))
	if err != nil {
		t.Fatalf("open cache client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close cache client: %v", err)
		}
	})
	increment := cache.NewScript("increment", "return redis.call('INCR', KEYS[1])")

	if _, err := client.RunScript(t.Context(), increment, []string{client.Key("counter")}); err == nil {
		t.Fatal("RunScript returned no error for a dropped connection")
	}

	if sent := server.scriptsSent.Load(); sent != 1 {
		t.Errorf("EVALSHA sent %d times for one RunScript call, want 1", sent)
	}
}

func TestRunScriptPassesKeysAndArguments(t *testing.T) {
	t.Parallel()
	client := cachetest.NewClient(t)
	echo := cache.NewScript("echo", "return {KEYS[1], ARGV[1], ARGV[2]}")
	key := client.Key("echo")

	assertScriptResult(t, client, echo, []string{key}, []any{key, "first", "7"}, "first", 7)
}

func TestRunScriptErrorNamesScript(t *testing.T) {
	t.Parallel()
	client := cachetest.NewClient(t)
	failing := cache.NewScript("failing", "return redis.error_reply('broken')")

	_, err := client.RunScript(t.Context(), failing, nil)

	if err == nil || !strings.HasPrefix(err.Error(), "run script failing: ") {
		t.Errorf("RunScript error = %v, want it to start with run script failing", err)
	}
}

func assertScriptResult(t *testing.T, client *cache.Client, script *cache.Script, keys []string, want any, arguments ...any) {
	t.Helper()
	got, err := client.RunScript(t.Context(), script, keys, arguments...)
	if err != nil {
		t.Fatalf("run script: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("script result mismatch (-want +got):\n%s", diff)
	}
}

func newReplyDroppingServer(t *testing.T) *replyDroppingServer {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := &replyDroppingServer{url: &url.URL{Scheme: "redis", Host: listener.Addr().String()}}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go server.serve(connection)
		}
	}()
	return server
}

func (server *replyDroppingServer) serve(connection net.Conn) {
	defer func() { _ = connection.Close() }()
	reader := bufio.NewReader(connection)
	for {
		command, err := readCommand(reader)
		if err != nil {
			return
		}
		switch strings.ToUpper(command[0]) {
		case "PING":
			_, err = io.WriteString(connection, "+PONG\r\n")
		case "EVALSHA":
			server.scriptsSent.Add(1)
			return
		default:
			_, err = io.WriteString(connection, "-ERR unknown command\r\n")
		}
		if err != nil {
			return
		}
	}
}

func readCommand(reader *bufio.Reader) ([]string, error) {
	count, err := readLength(reader, "*")
	if err != nil {
		return nil, err
	}
	command := make([]string, count)
	for index := range command {
		length, err := readLength(reader, "$")
		if err != nil {
			return nil, err
		}
		argument := make([]byte, length+len("\r\n"))
		if _, err := io.ReadFull(reader, argument); err != nil {
			return nil, err
		}
		command[index] = string(argument[:length])
	}
	return command, nil
}

func readLength(reader *bufio.Reader, marker string) (int, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, marker), "\r\n"))
}
