package bus

import (
	"sync"

	"alice/internal/agent"
	"alice/internal/domain"
	"alice/internal/platform"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"
)

// Runtime is the core event bus that processes events and manages state.
type Runtime struct {
	store       *store.Store
	policy      *policy.Engine
	workflow    *workflow.Runtime
	routeKeys   domain.RouteKeyEncoder
	idgen       domain.IDGenerator
	clock       platform.Clock
	shardCount  int
	mu          sync.Mutex
	seqByAgg    map[string]uint64
	hlcCounter  uint64
	routeByReq  map[string][]string
	onCritical  func(error)
	directAgent *agent.DirectAnswerExecutor
	logger      Logger
}

// NewRuntime creates a new Runtime.
func NewRuntime(s *store.Store, p *policy.Engine, wf *workflow.Runtime, idgen domain.IDGenerator, cfg Config, logger Logger) *Runtime {
	if cfg.ShardCount <= 0 {
		cfg.ShardCount = 16
	}
	if logger == nil {
		logger = platform.NewNoopLogger()
	}
	return &Runtime{
		store:      s,
		policy:     p,
		workflow:   wf,
		routeKeys:  domain.NewCanonicalRouteKeyEncoder(),
		idgen:      idgen,
		clock:      platform.RealClock{},
		shardCount: cfg.ShardCount,
		seqByAgg:   map[string]uint64{},
		routeByReq: map[string][]string{},
		logger:     logger.WithComponent("bus"),
	}
}

// Indexes returns the store indexes for external access.
func (r *Runtime) Indexes() *store.BoltIndexStore {
	return r.store.Indexes
}
