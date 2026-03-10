package domain

import (
	"crypto/rand"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	IDPrefixEvent       = "evt_"
	IDPrefixRequest     = "req_"
	IDPrefixDecision    = "dec_"
	IDPrefixTask        = "task_"
	IDPrefixBinding     = "bind_"
	IDPrefixExecution   = "exec_"
	IDPrefixArtifact    = "art_"
	IDPrefixContextPack = "ctx_"
	IDPrefixDispatch    = "disp_"
	IDPrefixApproval    = "apr_"
	IDPrefixOutbox      = "obx_"
	IDPrefixReply       = "rpl_"
	IDPrefixResult      = "res_"
	IDPrefixSchedule    = "sch_"
)

type IDGenerator interface {
	New(prefix string) string
}

type ULIDGenerator struct {
	mu      sync.Mutex
	entropy io.Reader
}

func NewULIDGenerator() *ULIDGenerator {
	return &ULIDGenerator{entropy: ulid.Monotonic(rand.Reader, 0)}
}

func (g *ULIDGenerator) New(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return fmt.Sprintf("%s%s", prefix, ulid.MustNew(ulid.Timestamp(time.Now().UTC()), g.entropy))
}
