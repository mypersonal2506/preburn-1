local reservation_key = KEYS[1]
local reservations_expiring_key = KEYS[2]
local counter_key = KEYS[3]
local pending_key = KEYS[4]
local open_reservations_key = KEYS[5]
local counters_ready_key = KEYS[6]
local change_id = ARGV[1]
local feature = ARGV[2]
local actual_amount = ARGV[3]
local counter_expire_at_unix = ARGV[4]

-- Negates the decimal string itself: tonumber loses precision above 2^53, and
-- HINCRBY rejects "-0".
local function negate(value)
  if value == '0' then
    return value
  end
  if string.sub(value, 1, 1) == '-' then
    return string.sub(value, 2)
  end
  return '-' .. value
end

if redis.call('EXISTS', counters_ready_key) == 0 then
  redis.call('ZREM', pending_key, change_id)
  return {'not_ready'}
end
if redis.call('ZREM', pending_key, change_id) == 0 then
  return {'not_pending'}
end

local decision_id = string.match(reservation_key, '[^:]+$')
local reservation = redis.call('HMGET', reservation_key, 'status', 'amount')
if reservation[1] == 'reserved' then
  local reserved_amount = negate(reservation[2])
  redis.call('HINCRBY', counter_key, 'reserved', reserved_amount)
  redis.call('HINCRBY', counter_key, 'reserved:' .. feature, reserved_amount)
  redis.call('HSET', reservation_key, 'status', 'settled')
  redis.call('SREM', open_reservations_key, decision_id)
  redis.call('ZREM', reservations_expiring_key, decision_id)
end
redis.call('HINCRBY', counter_key, 'settled', actual_amount)
redis.call('HINCRBY', counter_key, 'settled:' .. feature, actual_amount)
redis.call('EXPIREAT', counter_key, counter_expire_at_unix)
return {'settled'}
