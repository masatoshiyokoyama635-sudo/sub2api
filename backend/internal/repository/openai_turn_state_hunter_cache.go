package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const codexHunterCachePrefix = "codex:hunter:candidate:"

var putCodexHunterCandidate = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
if old then
  local ok, value = pcall(cjson.decode, old)
  if ok and type(value) == 'table' then
    if value.rejected == true and value.value == ARGV[4] then return 0 end
    if type(value.issued_unix) == 'number' and type(value.expires_unix) == 'number'
      and type(value.value) == 'string' and value.expires_unix > tonumber(ARGV[5])
      and value.issued_unix <= tonumber(ARGV[5])
      and (value.issued_unix >= tonumber(ARGV[2]) or value.value == ARGV[4]) then return 0 end
  end
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[3])
return 1
`)

var deleteCodexHunterCandidate = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
if ARGV[1] == '' then return redis.call('DEL', KEYS[1]) end
if not old then
  redis.call('SET', KEYS[1], cjson.encode({value=ARGV[1], rejected=true, issued_unix=0, expires_unix=tonumber(ARGV[2])+3600}), 'PX', 3600000)
  return 1
end
local ok, value = pcall(cjson.decode, old)
if ok and type(value) == 'table' and value.value == ARGV[1] then
  local ttl = redis.call('PTTL', KEYS[1])
  if ttl <= 0 or ttl > 3600000 then ttl = 3600000 end
  value.rejected = true
  redis.call('SET', KEYS[1], cjson.encode(value), 'PX', ttl)
  return 1
end
return 0
`)

func (c *gatewayCache) GetCodexHunterCandidate(ctx context.Context, key string) (*service.CodexHunterCandidate, error) {
	data, err := c.rdb.Get(ctx, codexHunterCachePrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value service.CodexHunterCandidate
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if !service.ValidCodexHunterCandidate(value, time.Now()) {
		return nil, nil
	}
	return &value, nil
}

func (c *gatewayCache) PutCodexHunterCandidate(ctx context.Context, key string, value service.CodexHunterCandidate) error {
	now := time.Now()
	ttl := time.Unix(value.ExpiresUnix, 0).Sub(now)
	if !service.ValidCodexHunterCandidate(value, now) || ttl.Milliseconds() < 1 {
		return fmt.Errorf("invalid or expired Codex hunter candidate")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return putCodexHunterCandidate.Run(ctx, c.rdb, []string{codexHunterCachePrefix + key}, data, value.IssuedUnix, ttl.Milliseconds(), value.Value, now.Unix()).Err()
}

func (c *gatewayCache) DeleteCodexHunterCandidate(ctx context.Context, key, value string) error {
	return deleteCodexHunterCandidate.Run(ctx, c.rdb, []string{codexHunterCachePrefix + key}, value, time.Now().Unix()).Err()
}

var _ service.CodexHunterCandidateStore = (*gatewayCache)(nil)
