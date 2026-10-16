package main

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// Sword 1.3.2: Utility method 9 is sent right after AcquireNexUniqueId (caller main+0x6ae140).
// Request builder 0x1764b70 writes one u8; response decoder 0x17650a0 reads one u64.
// Unanswered, the client shows 2306-0103. The id is the PID, like the shared method 1.
func utilityHandler() func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	shared := nex.UtilityHandler()
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if req.Method != 9 {
			return shared(c, req)
		}
		if len(req.Body) != 1 {
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
		}
		out := nex.NewStreamOut(c.Settings)
		out.U64(uint64(c.PID))
		fmt.Printf("[Utility] method 9 pid=%d type=%d -> %d\n", c.PID, req.Body[0], c.PID)
		return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
	}
}
