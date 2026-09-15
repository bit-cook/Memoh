package native

import "context"

type feedbackIndexesKey struct{}

// InternalFeedbackIndexes names internal model inputs in the decorated step.
// The sidecar stays outside SDK messages: model roles describe the provider
// protocol, not whether a person opened a new conversation turn.
func InternalFeedbackIndexes(ctx context.Context) []int {
	indexes, _ := ctx.Value(feedbackIndexesKey{}).([]int)
	return append([]int(nil), indexes...)
}

func (s *readMediaDecorationState) withMessageOrigins(ctx context.Context, step int) context.Context {
	if s == nil {
		return ctx
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var indexes []int
	for _, injection := range s.injections {
		if injection.admitted && injection.afterStep+1 == step {
			indexes = append(indexes, injection.durableIndex)
		}
	}
	return context.WithValue(ctx, feedbackIndexesKey{}, indexes)
}
