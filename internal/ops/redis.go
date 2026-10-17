package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// RedisOps operates on a Redis instance via go-redis (host endpoint).
type RedisOps struct{}

func (o *RedisOps) client(inst *domain.Instance) (*redis.Client, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return nil, fmt.Errorf("no endpoint")
	}
	return redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", ep.Host, ep.Port),
		Password: inst.Cred.Password,
	}), nil
}

// Scan returns up to limit keys matching pattern.
func (o *RedisOps) Scan(ctx context.Context, inst *domain.Instance, pattern string, limit int) ([]string, error) {
	c, err := o.client(inst)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if pattern == "" {
		pattern = "*"
	}
	if limit <= 0 {
		limit = 100
	}
	var keys []string
	var cursor uint64
	for {
		batch, next, err := c.Scan(ctx, cursor, pattern, 50).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 || len(keys) >= limit {
			break
		}
	}
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys, nil
}

// Get returns a key's value.
func (o *RedisOps) Get(ctx context.Context, inst *domain.Instance, key string) (string, error) {
	c, err := o.client(inst)
	if err != nil {
		return "", err
	}
	defer c.Close()
	v, err := c.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key %q not found", key)
	}
	return v, err
}

// Set sets a key, with optional TTL (0 = no expiry).
func (o *RedisOps) Set(ctx context.Context, inst *domain.Instance, key, value string, ttl time.Duration) error {
	c, err := o.client(inst)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Set(ctx, key, value, ttl).Err()
}

// Del removes keys, returning how many were deleted.
func (o *RedisOps) Del(ctx context.Context, inst *domain.Instance, keys ...string) (int64, error) {
	c, err := o.client(inst)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	return c.Del(ctx, keys...).Result()
}

// Info returns the server INFO as raw text.
func (o *RedisOps) Info(ctx context.Context, inst *domain.Instance) (string, error) {
	c, err := o.client(inst)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.Info(ctx).Result()
}

// FlushDB removes all keys in the current database.
func (o *RedisOps) FlushDB(ctx context.Context, inst *domain.Instance) error {
	c, err := o.client(inst)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.FlushDB(ctx).Err()
}
