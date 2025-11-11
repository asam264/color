package strategy

import (
	"context"
)

// Strategy 路由策略接口
type Strategy interface {
	// Select 根据 color 选择目标地址
	Select(ctx context.Context, color string) (string, error)
}
