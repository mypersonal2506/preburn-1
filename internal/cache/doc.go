// Package cache connects to Redis or Valkey. Client.Key builds every key from
// PREBURN_REDIS_KEY_PREFIX. Lua scripts run by their digest and reload when
// the server has lost them. Processes announce stale data on the invalidate
// channel, and LocalCache keeps in-process copies with a time to live and a
// maximum entry count.
package cache
