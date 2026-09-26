local pending_key = KEYS[1]
local counters_ready_key = KEYS[2]
local change_id = ARGV[1]
local started_at_milliseconds = ARGV[2]
local counter_expire_at_unix = ARGV[3]

if redis.call('EXISTS', counters_ready_key) == 0 then
  return {'not_ready'}
end
redis.call('ZADD', pending_key, started_at_milliseconds, change_id)
redis.call('EXPIREAT', pending_key, counter_expire_at_unix)
return {'pending'}
