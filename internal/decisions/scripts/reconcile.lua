local counter_key = KEYS[1]
local pending_key = KEYS[2]
local open_reservations_key = KEYS[3]
local reservations_expiring_key = KEYS[4]
local counters_ready_key = KEYS[5]
local abandoned_until_milliseconds = ARGV[1]
local counter_expire_at_unix = ARGV[2]
local reservation_key_prefix = ARGV[3]
local finished_end = 4 + 2 * tonumber(ARGV[4])

local part_size = 1000000000

-- Amounts stay exact above 2^53 by summing their digits in two parts of nine
-- digits each, like the reserve script.
local function add(sums, field, value)
  local sum = sums[field] or {0, 0}
  local high_length = string.len(value) - 9
  if high_length > 0 then
    sum[1] = sum[1] + tonumber(string.sub(value, 1, high_length))
    sum[2] = sum[2] + tonumber(string.sub(value, high_length + 1))
  else
    sum[2] = sum[2] + tonumber(value)
  end
  local carry = math.floor(sum[2] / part_size)
  sum[1] = sum[1] + carry
  sum[2] = sum[2] - carry * part_size
  sums[field] = sum
end

local function format(sum)
  if sum[1] == 0 then
    return string.format('%d', sum[2])
  end
  return string.format('%d%09d', sum[1], sum[2])
end

if redis.call('EXISTS', counters_ready_key) == 0 then
  return {'skipped', 'not_ready'}
end
redis.call('ZREMRANGEBYSCORE', pending_key, '-inf', abandoned_until_milliseconds)
if redis.call('ZCARD', pending_key) > 0 then
  return {'skipped', 'pending'}
end

local written = 0
for index = 5, finished_end, 2 do
  local decision_id = ARGV[index]
  local reservation_key = reservation_key_prefix .. decision_id
  if redis.call('HGET', reservation_key, 'status') == 'reserved' then
    redis.call('HSET', reservation_key, 'status', ARGV[index + 1])
    redis.call('SREM', open_reservations_key, decision_id)
    redis.call('ZREM', reservations_expiring_key, decision_id)
    written = written + 1
  end
end

local reserved = {reserved = {0, 0}}
for _, decision_id in ipairs(redis.call('SMEMBERS', open_reservations_key)) do
  local reservation = redis.call('HMGET', reservation_key_prefix .. decision_id, 'status', 'feature', 'amount')
  if reservation[1] == 'reserved' then
    add(reserved, 'reserved', reservation[3])
    add(reserved, 'reserved:' .. reservation[2], reservation[3])
  else
    redis.call('SREM', open_reservations_key, decision_id)
  end
end
local current = redis.call('HGETALL', counter_key)
for index = 1, #current, 2 do
  local field = current[index]
  if string.sub(field, 1, 9) == 'reserved:' and reserved[field] == nil then
    reserved[field] = {0, 0}
  end
end
for field, sum in pairs(reserved) do
  local expected = format(sum)
  if (redis.call('HGET', counter_key, field) or '0') ~= expected then
    redis.call('HSET', counter_key, field, expected)
    written = written + 1
  end
end

for index = finished_end + 1, #ARGV, 3 do
  local field = ARGV[index]
  -- A missing field reads as the empty string, which is how the caller sends
  -- a field it read as missing.
  if (redis.call('HGET', counter_key, field) or '') == ARGV[index + 1] then
    redis.call('HSET', counter_key, field, ARGV[index + 2])
    written = written + 1
  end
end
if written > 0 then
  redis.call('EXPIREAT', counter_key, counter_expire_at_unix)
end
return {'reconciled', tostring(written)}
