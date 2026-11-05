// Package chatsend 标记出站聊天消息的来源，供 engine 与 automation 共享，
// 避免 automation → engine 的循环依赖。
package chatsend

import "context"

type autoKey struct{}

// WithAuto 标记本次发送来自 AI 助手或自动化，而非真人手动发送。
// 只有真人手动回复才会让 AI 助手让位，必须把两者区分开。
func WithAuto(ctx context.Context) context.Context {
	return context.WithValue(ctx, autoKey{}, true)
}

// IsAuto 判断本次发送是否来自 AI 助手或自动化。
func IsAuto(ctx context.Context) bool {
	auto, _ := ctx.Value(autoKey{}).(bool)
	return auto
}
