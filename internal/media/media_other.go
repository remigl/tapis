//go:build !windows && !linux

package media

import "context"

func watch(ctx context.Context, fn func(Info)) {}
