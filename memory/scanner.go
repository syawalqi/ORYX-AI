// Package memory provides server context scanning for the agent loop.
// It collects system info, service states, disk usage, network config,
// and security posture — rendered as markdown for the LLM's system prompt.
package memory

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/syawalqi/flare/executor"
)
