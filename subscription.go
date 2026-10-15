package main

import (
	"encoding/binary"
	nex "github.com/NextendoNetwork/nextendo-nex"
	"sync"
)

// Sword 1.3.2: protocol constructor main+0x1760870 selects 0x75.
// Method 1 writes u32, framed SubscriptionData (u64 + string), bool.
// Its response decoder at 0x1762c30 reads no fields. Method 8 has no
// arguments and decodes a vector of SubscriptionData via 0x1763190.
// This implements the initial per-player subscription registry only;
// cross-player publication and the remaining methods are not implemented.
func subscriptionHandler() func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	var mu sync.Mutex
	entries := make(map[uint64]map[uint32][]byte)
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		invalid := func() *nex.RMCMessage {
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
		}
		switch req.Method {
		case 1:
			b := req.Body
			if len(b) < 10 {
				return invalid()
			}
			size := uint64(binary.LittleEndian.Uint32(b[5:9]))
			if size < 10 || size+10 != uint64(len(b)) || b[len(b)-1] > 1 {
				return invalid()
			}
			// Retain the complete versioned structure, including its wire header.
			channel := binary.LittleEndian.Uint32(b[:4])
			mu.Lock()
			if entries[c.PID] == nil {
				entries[c.PID] = make(map[uint32][]byte)
			}
			entries[c.PID][channel] = append([]byte(nil), b[4:len(b)-1]...)
			mu.Unlock()
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, nil)
		case 8:
			if len(req.Body) != 0 {
				return invalid()
			}
			out := nex.NewStreamOut(c.Settings)
			mu.Lock()
			out.U32(uint32(len(entries[c.PID])))
			for _, data := range entries[c.PID] {
				out.Write(data)
			}
			mu.Unlock()
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
		default:
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreNotImplemented)
		}
	}
}
