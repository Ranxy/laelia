package executor

import (
	"time"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

type Request struct {
	CommandID      string
	Command        string
	Instruction    string
	Profile        string
	WorkingDir     string
	Env            map[string]string
	TimeoutSeconds int32
	ExecutorKind   v1pb.ExecutorKind
	AllowDiff      bool
}

type Event struct {
	SeqNo      int32
	Type       v1pb.CommandEventType
	Summary    string
	Payload    map[string]any
	Text       string
	StreamType v1pb.CommandOutput_StreamType
	Timestamp  time.Time
}

type Runtime interface {
	Start()
	Cancel()
	OutputChannel() <-chan OutputChunk
	EventChannel() <-chan Event
	ResultChannel() <-chan Result
	Done() <-chan struct{}
}

type PermissionResolver interface {
	ResolvePermission(optionID string)
}
