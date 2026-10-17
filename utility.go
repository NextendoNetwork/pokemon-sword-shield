package main

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// Sword 1.3.2: Utility method 9 is sent right after AcquireNexUniqueId (caller main+0x6ae140).
// Request builder 0x1764b70 writes one u8; response decoder 0x17650a0 reads one u64.
// Unanswered, the client shows 2306-0103. The id is the PID, like the shared method 1.
//
// Battle Stadium sends GetIntegerSettings(0) from Ranked prep (0xb3d610) and matching (0xb49e30) and
// GetIntegerSettings(1) from 0xb47884; all consumers (0xb3ed40, 0xb4a3e0, 0xb480c0) read the same
// keys, so both indexes get the same table. Keys 0x32
// and 0x33 must both exist or the game shows a communication error; they are the Singles (0) and
// Doubles (1) rule ids for RequestGetRankMatchSetting (0x112d7a0). Ids 1..9 load the game's own
// bin/battle/regulation/regulation_preset_core_<id>.bin (0x8feac0); ids above 9 would be fetched
// with MatchmakeExtension 57. Preset 1 is Singles Lv.50, preset 4 is Doubles Lv.50.
// Keys 1..14 are parsed into slots starting at key 1 (0xb3ef10). Key 1 is the matchmaking search
// time (converted to a double for the matching job, 0xb2d794); without it matching fails at once with
// "An opposing Trainer couldn't be found" (0xb2d4ec, confirmed in-game). Key 2 is the number of
// matching attempts (0xb2d2e0, default 1).
var battleStadiumSettings = map[uint16]int32{1: 60, 2: 3, 0x32: 1, 0x33: 4}

func utilityHandler() func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	shared := nex.UtilityHandler()
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if req.Method == nex.MethodGetIntegerSettings {
			in := nex.NewStreamIn(req.Body, c.Settings)
			index := in.U32()
			if in.Err() != nil {
				return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
			}
			table := map[uint16]int32{}
			if index <= 1 {
				table = battleStadiumSettings
			}
			out := nex.NewStreamOut(c.Settings)
			nex.WriteMap(out, table, func(o *nex.StreamOut, k uint16) { o.U16(k) }, func(o *nex.StreamOut, v int32) { o.S32(v) })
			fmt.Printf("[Utility] GetIntegerSettings pid=%d index=%d -> %d entries\n", c.PID, index, len(table))
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
		}
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
