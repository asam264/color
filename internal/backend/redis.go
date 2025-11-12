package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisKeyPrefix = "colorproxy:route:"

type RedisBackend struct {
	client *redis.Client
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func NewRedisBackend(cfg *RedisConfig) (*RedisBackend, error) {
	if cfg.Addr == "" {
		cfg.Addr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &RedisBackend{client: client}, nil
}

func (b *RedisBackend) Register(ctx context.Context, route *Route, ttl time.Duration) error {
	key := redisKeyPrefix + route.Color
	route.ExpiresAt = time.Now().Add(ttl)

	data, err := json.Marshal(route)
	if err != nil {
		return err
	}

	return b.client.Set(ctx, key, data, ttl).Err()
}

func (b *RedisBackend) Get(ctx context.Context, color string) (*Route, error) {
	key := redisKeyPrefix + color
	data, err := b.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, errors.New("route not found")
		}
		return nil, err
	}

	var route Route
	if err := json.Unmarshal([]byte(data), &route); err != nil {
		return nil, err
	}

	return &route, nil
}

func (b *RedisBackend) Heartbeat(ctx context.Context, color, address, token string, ttl time.Duration) error {
	route, err := b.Get(ctx, color)
	if err != nil {
		return err
	}

	if route.Address != address || route.Token != token {
		return errors.New("address or token mismatch")
	}

	route.ExpiresAt = time.Now().Add(ttl)
	return b.Register(ctx, route, ttl)
}

func (b *RedisBackend) List(ctx context.Context) ([]*Route, error) {
	keys, err := b.client.Keys(ctx, redisKeyPrefix+"*").Result()
	if err != nil {
		return nil, err
	}

	routes := make([]*Route, 0, len(keys))
	for _, key := range keys {
		data, err := b.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var route Route
		if err := json.Unmarshal([]byte(data), &route); err != nil {
			continue
		}

		routes = append(routes, &route)
	}

	return routes, nil
}

func (b *RedisBackend) Delete(ctx context.Context, color string) error {
	key := redisKeyPrefix + color
	return b.client.Del(ctx, key).Err()
}

func (b *RedisBackend) DeleteExpired(ctx context.Context) error {
	routes, err := b.List(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, route := range routes {
		if now.After(route.ExpiresAt) {
			b.Delete(ctx, route.Color)
		}
	}

	return nil
}

func (b *RedisBackend) Close() error {
	return b.client.Close()
}
