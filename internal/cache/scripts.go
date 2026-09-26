package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const noScriptErrorPrefix = "NOSCRIPT"

// Script is a Lua script that a Client runs by its SHA-1 digest. Create one
// with NewScript.
type Script struct {
	name        string
	redisScript *redis.Script
}

// NewScript returns a Script for the Lua source. Errors from loading or
// running it carry name.
func NewScript(name, source string) *Script {
	return &Script{name: name, redisScript: redis.NewScript(source)}
}

// LoadScripts loads every script into the server's script cache with SCRIPT
// LOAD and stops at the first failure. Once a call returns nil, ScriptsLoaded
// reports true.
func (client *Client) LoadScripts(ctx context.Context, scripts ...*Script) error {
	for _, script := range scripts {
		if err := script.redisScript.Load(ctx, client.redis).Err(); err != nil {
			return fmt.Errorf("load script %s: %w", script.name, err)
		}
	}
	client.scriptsLoaded.Store(true)
	return nil
}

// RunScript runs script with EVALSHA on keys and arguments and returns its
// reply. When the server answers NOSCRIPT, because it restarted or flushed its
// script cache, RunScript loads the script and retries once. A reply lost to
// a timeout or a closed connection returns an error and the script is not
// sent again, so it ran once or not at all.
func (client *Client) RunScript(ctx context.Context, script *Script, keys []string, arguments ...any) (any, error) {
	reply, err := script.redisScript.EvalSha(ctx, client.redis, keys, arguments...).Result()
	if redis.HasErrorPrefix(err, noScriptErrorPrefix) {
		if err := script.redisScript.Load(ctx, client.redis).Err(); err != nil {
			return nil, fmt.Errorf("reload script %s: %w", script.name, err)
		}
		reply, err = script.redisScript.EvalSha(ctx, client.redis, keys, arguments...).Result()
	}
	if err != nil {
		return nil, fmt.Errorf("run script %s: %w", script.name, err)
	}
	return reply, nil
}

// ScriptsLoaded reports whether a LoadScripts call has returned nil. The
// readiness check fails until it does.
func (client *Client) ScriptsLoaded() bool {
	return client.scriptsLoaded.Load()
}
