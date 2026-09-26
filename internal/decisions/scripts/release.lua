local reservation_key = KEYS[1]
local reservations_expiring_key = KEYS[2]
local counters_ready_key = KEYS[3]
local next_status = ARGV[1]
local open_reservations_suffix = ARGV[2]

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
  return {'not_ready'}
end

local reservation = redis.call('HMGET', reservation_key, 'status', 'counter_key', 'feature', 'amount')
local status = reservation[1]
if status ~= 'reserved' then
  return {'noop', status or 'missing'}
end
local counter_key = reservation[2]
local feature = reservation[3]
local reserved_amount = negate(reservation[4])
local decision_id = string.match(reservation_key, '[^:]+$')

redis.call('HINCRBY', counter_key, 'reserved', reserved_amount)
redis.call('HINCRBY', counter_key, 'reserved:' .. feature, reserved_amount)
redis.call('HSET', reservation_key, 'status', next_status)
redis.call('SREM', counter_key .. open_reservations_suffix, decision_id)
redis.call('ZREM', reservations_expiring_key, decision_id)
return {next_status}
