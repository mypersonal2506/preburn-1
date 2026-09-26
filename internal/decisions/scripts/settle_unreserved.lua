local counter_key = KEYS[1]
local pending_key = KEYS[2]
local counters_ready_key = KEYS[3]
local change_id = ARGV[1]
local feature = ARGV[2]
local amount = ARGV[3]
local count_increment = ARGV[4]
local counter_expire_at_unix = ARGV[5]

if redis.call('EXISTS', counters_ready_key) == 0 then
  redis.call('ZREM', pending_key, change_id)
  return {'not_ready'}
end
if redis.call('ZREM', pending_key, change_id) == 0 then
  return {'not_pending'}
end

redis.call('HINCRBY', counter_key, 'settled', amount)
redis.call('HINCRBY', counter_key, 'settled:' .. feature, amount)
redis.call('HINCRBY', counter_key, 'count', count_increment)
redis.call('HINCRBY', counter_key, 'count:' .. feature, count_increment)
redis.call('EXPIREAT', counter_key, counter_expire_at_unix)
return {'settled'}
