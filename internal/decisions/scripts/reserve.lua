local counter_key = KEYS[1]
local reservation_key = KEYS[2]
local reservations_expiring_key = KEYS[3]
local counters_ready_key = KEYS[4]
local pending_key = KEYS[5]
local open_reservations_key = KEYS[6]
local mode = ARGV[1]
local decision_id = ARGV[2]
local feature = ARGV[3]
local amount = ARGV[4]
local allowance_ceiling = ARGV[5]
local feature_amount_ceiling = ARGV[6]
local feature_count_ceiling = ARGV[7]
local expires_at_unix = ARGV[8]
local counter_expire_at_unix = ARGV[9]
local reservation_expire_at_unix = ARGV[10]
local total_amount_ceiling = ARGV[11]
local total_count_ceiling = ARGV[12]
local decided_at_milliseconds = ARGV[13]

local part_size = 1000000000

-- Lua numbers are doubles, exact only up to 2^53 (about 9 million USD in
-- nanos). Values stay decimal strings and are summed in two exact parts,
-- because adding them with tonumber would misjudge larger ceilings.
local function split(value)
  local negative = string.sub(value, 1, 1) == '-'
  local digits = negative and string.sub(value, 2) or value
  local high_length = string.len(digits) - 9
  local high = 0
  local low = tonumber(digits)
  if high_length > 0 then
    high = tonumber(string.sub(digits, 1, high_length))
    low = tonumber(string.sub(digits, high_length + 1))
  end
  if negative then
    return -high, -low
  end
  return high, low
end

local function sum_exceeds(values, ceiling)
  local ceiling_high, ceiling_low = split(ceiling)
  local high = -ceiling_high
  local low = -ceiling_low
  for _, value in ipairs(values) do
    local value_high, value_low = split(value)
    high = high + value_high
    low = low + value_low
  end
  local carry = math.floor(low / part_size)
  high = high + carry
  low = low - carry * part_size
  return high > 0 or (high == 0 and low > 0)
end

if redis.call('EXISTS', counters_ready_key) == 0 then
  return {'not_ready'}
end

local settled_feature_field = 'settled:' .. feature
local reserved_feature_field = 'reserved:' .. feature
local count_feature_field = 'count:' .. feature
local current = redis.call('HMGET', counter_key, 'settled', 'reserved', 'count', settled_feature_field, reserved_feature_field, count_feature_field)
local settled = current[1] or '0'
local reserved = current[2] or '0'
local count = current[3] or '0'
local settled_feature = current[4] or '0'
local reserved_feature = current[5] or '0'
local count_feature = current[6] or '0'

if allowance_ceiling ~= '' and sum_exceeds({settled, reserved, amount}, allowance_ceiling) then
  return {'denied', 'allowance'}
end
if feature_amount_ceiling ~= '' and sum_exceeds({settled_feature, reserved_feature, amount}, feature_amount_ceiling) then
  return {'denied', 'limit'}
end
if feature_count_ceiling ~= '' and sum_exceeds({count_feature, '1'}, feature_count_ceiling) then
  return {'denied', 'limit'}
end
if total_amount_ceiling ~= '' and sum_exceeds({settled, reserved, amount}, total_amount_ceiling) then
  return {'denied', 'limit'}
end
if total_count_ceiling ~= '' and sum_exceeds({count, '1'}, total_count_ceiling) then
  return {'denied', 'limit'}
end

redis.call('HINCRBY', counter_key, 'count', 1)
redis.call('HINCRBY', counter_key, count_feature_field, 1)
redis.call('EXPIREAT', counter_key, counter_expire_at_unix)
redis.call('ZADD', pending_key, decided_at_milliseconds, decision_id)
redis.call('EXPIREAT', pending_key, counter_expire_at_unix)
if mode == 'count_only' then
  return {'counted'}
end

redis.call('HINCRBY', counter_key, 'reserved', amount)
redis.call('HINCRBY', counter_key, reserved_feature_field, amount)
redis.call('HSET', reservation_key, 'counter_key', counter_key, 'feature', feature, 'amount', amount, 'status', 'reserved', 'expires_at_unix', expires_at_unix)
redis.call('EXPIREAT', reservation_key, reservation_expire_at_unix)
redis.call('SADD', open_reservations_key, decision_id)
redis.call('EXPIREAT', open_reservations_key, counter_expire_at_unix)
redis.call('ZADD', reservations_expiring_key, expires_at_unix, decision_id)
return {'reserved'}
