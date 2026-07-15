package observability

import "context"

type Span interface {
	End(error)
}

type Tracer interface {
	Start(context.Context, string) (context.Context, Span)
}

type NoopTracer struct{}

type noopSpan struct{}

func (NoopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, noopSpan{}
}

func (noopSpan) End(error) {}
