package main

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// Sword 1.3.2: MatchmakeExtension method 55 is the Battle Stadium rule set query (request builder
// main+0x16b5400, decoder 0x16b8c70 -> 0x16bcad0). The shared stack answers 55 as SSBU's arena
// code, which the game cannot decode, so "Checking the data..." spins forever. The reply is a
// structure {u8 version, u32 size, List<RegulationIndex>, u32} with no Data parent header; no
// official rule sets exist any more, so the list is empty.
func matchmakeExtensionHandler(shared func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage) func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if req.Method != 55 {
			return shared(c, req)
		}
		fmt.Printf("[Regulation] pid=%d method 55 body=%x -> no rule sets\n", c.PID, req.Body)
		out := nex.NewStreamOut(c.Settings)
		out.Write([]byte{0})
		out.U32(8)
		out.U32(0)
		out.U32(0)
		return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
	}
}
