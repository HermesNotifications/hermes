package tracing

import (
	"os"

	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"github.com/DataDog/dd-trace-go/v2/profiler"
)

// Start initializes the Datadog tracer and profiler.
// No-ops if DD_AGENT_HOST is not set, making instrumentation safe
// to compile in but inactive without an agent.
func Start() {
	if os.Getenv("DD_AGENT_HOST") == "" {
		return
	}

	_ = tracer.Start()

	_ = profiler.Start(
		profiler.WithProfileTypes(
			profiler.CPUProfile,
			profiler.HeapProfile,
			profiler.GoroutineProfile,
		),
	)
}

// Shutdown flushes and stops the tracer and profiler.
func Shutdown() {
	profiler.Stop()
	tracer.Stop()
}
