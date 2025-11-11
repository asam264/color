package strategy

import (
	"context"

	"github.com/asam264/color/internal/backend"
)

// SimpleStrategy 简单策略：直接返回匹配的地址
type SimpleStrategy struct {
	backend backend.Backend
}

func NewSimpleStrategy(backend backend.Backend) *SimpleStrategy {
	return &SimpleStrategy{backend: backend}
}

func (s *SimpleStrategy) Select(ctx context.Context, color string) (string, error) {
	route, err := s.backend.Get(ctx, color)
	if err != nil {
		return "", err
	}
	return route.Address, nil
}
