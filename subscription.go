package main

import (
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// Sword 1.3.2 Subscription (0x75), from main and captured bodies:
//   - Methods 1 and 3 send u32 channel, an empty Data parent header, then SubscriptionData
//     {u8 version, u32 size, u64, u16-length 512-byte profile blob}; method 1 adds a bool.
//     The host re-sends it with method 3 after creating a raid session.
//   - Method 8 decodes a list of the caller's own SubscriptionData (0x1763190).
//   - Methods 3 and 15 decode nothing (0x1762c50, 0x1762e40); 15 is a ~2 s heartbeat.
//   - Method 16 sends the caller's own location structure {u64, u32 x4 (position), u16-length
//     200-byte blob, which carries a hosted raid's gathering id} plus u32 limit, and decodes a
//     list of the same structure (0x1763ce0 / 0x1762560): other players' latest ones.
//   - Method 17 sends List<u64 pid> (friends) and decodes the same list: those players' ones.
//   - Method 2 sends SubscriptionData like method 3; its decoder 0x1762c40 reads nothing.
const subscriptionActiveWindow = time.Minute

// Notification types 112000-112002 are handled by the subscription manager; 112002 = new data.
const subscriptionNewDataEvent uint32 = 112002

type subscriptionEntry struct {
	data []byte // Data header + SubscriptionData as sent
	blob []byte
}

// parseSubscriptionData splits a method 1/3 body; rest is whatever follows the structure.
func parseSubscriptionData(b []byte) (channel uint32, e subscriptionEntry, rest []byte, ok bool) {
	if len(b) < 24 || b[4] != 0 || binary.LittleEndian.Uint32(b[5:9]) != 0 {
		return
	}
	end := 14 + int(binary.LittleEndian.Uint32(b[10:14]))
	if end > len(b) || end < 24 || 24+int(binary.LittleEndian.Uint16(b[22:24])) != end {
		return
	}
	return binary.LittleEndian.Uint32(b[:4]), subscriptionEntry{data: b[4:end], blob: b[24:end]}, b[end:], true
}

func subscriptionHandler() func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	var mu sync.Mutex
	entries := make(map[uint64]map[uint32]subscriptionEntry)
	locations := make(map[uint64][]byte)
	lastSeen := make(map[uint64]time.Time)
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		invalid := func() *nex.RMCMessage {
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
		}
		mu.Lock()
		lastSeen[c.PID] = time.Now()
		mu.Unlock()
		switch req.Method {
		case 1, 2, 3:
			channel, e, rest, ok := parseSubscriptionData(req.Body)
			if !ok || (req.Method == 1 && (len(rest) != 1 || rest[0] > 1)) || (req.Method != 1 && len(rest) > 1) {
				return invalid()
			}
			e.data = append([]byte(nil), e.data...)
			e.blob = append([]byte(nil), e.blob...)
			mu.Lock()
			if entries[c.PID] == nil {
				entries[c.PID] = make(map[uint32]subscriptionEntry)
			}
			entries[c.PID][channel] = e
			var others []uint64
			for pid := range entries {
				if pid != c.PID && time.Since(lastSeen[pid]) < subscriptionActiveWindow {
					others = append(others, pid)
				}
			}
			mu.Unlock()
			// Type 112002 makes the client's subscription manager (0x11a84b0) fetch stamps via method 10.
			for _, pid := range others {
				if t := c.Endpoint.FindConnectionByPID(pid); t != nil {
					nex.SendNotification(t, &nex.NotificationEvent{PIDSource: c.PID, Type: subscriptionNewDataEvent})
				}
			}
			if req.Method == 1 && len(others) > 0 {
				nex.SendNotification(c, &nex.NotificationEvent{PIDSource: others[0], Type: subscriptionNewDataEvent})
			}
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, nil)
		case 10:
			// Three u32 (limit 50, 50, ?) -> list<ActivePlayerSubscriptionData> (0x17636f0):
			// the SubscriptionData, then its own header {u8 0, u32 1} and a bool.
			if len(req.Body) != 12 {
				return invalid()
			}
			limit := int(binary.LittleEndian.Uint32(req.Body[:4]))
			var elems [][]byte
			mu.Lock()
			pids := make([]uint64, 0, len(entries))
			for pid := range entries {
				if pid != c.PID && time.Since(lastSeen[pid]) < subscriptionActiveWindow {
					pids = append(pids, pid)
				}
			}
			sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
			for _, pid := range pids {
				for _, e := range entries[pid] {
					if len(elems) < limit {
						elems = append(elems, append(append([]byte(nil), e.data...), 0, 1, 0, 0, 0, 1))
					}
				}
			}
			mu.Unlock()
			out := nex.NewStreamOut(c.Settings)
			out.U32(uint32(len(elems)))
			for _, el := range elems {
				out.Write(el)
			}
			fmt.Printf("[Subscription] method 10 pid=%d -> %d record(s)\n", c.PID, len(elems))
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
		case 8:
			if len(req.Body) != 0 {
				return invalid()
			}
			out := nex.NewStreamOut(c.Settings)
			mu.Lock()
			out.U32(uint32(len(entries[c.PID])))
			for _, e := range entries[c.PID] {
				out.Write(e.data)
			}
			mu.Unlock()
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
		case 15:
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, nil)
		case 16:
			b := req.Body
			if len(b) < 22 || b[0] != 0 || binary.LittleEndian.Uint32(b[1:5]) != 0 ||
				10+int(binary.LittleEndian.Uint32(b[6:10]))+4 != len(b) {
				return invalid()
			}
			limit := int(binary.LittleEndian.Uint32(b[len(b)-4:]))
			var elems [][]byte
			mu.Lock()
			// The client sends its u64 as 0; others need it to be the owner's PID.
			loc := append([]byte(nil), b[:len(b)-4]...)
			binary.LittleEndian.PutUint64(loc[10:18], c.PID)
			locations[c.PID] = loc
			pids := make([]uint64, 0, len(locations))
			for pid := range locations {
				if pid != c.PID && time.Since(lastSeen[pid]) < subscriptionActiveWindow {
					pids = append(pids, pid)
				}
			}
			sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
			for _, pid := range pids {
				if len(elems) >= limit {
					break
				}
				elems = append(elems, locations[pid])
			}
			mu.Unlock()
			out := nex.NewStreamOut(c.Settings)
			out.U32(uint32(len(elems)))
			for _, el := range elems {
				out.Write(el)
			}
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
		case 17:
			b := req.Body
			if len(b) < 4 {
				return invalid()
			}
			n := int(binary.LittleEndian.Uint32(b[:4]))
			if 4+8*n != len(b) {
				return invalid()
			}
			var elems [][]byte
			mu.Lock()
			for i := 0; i < n; i++ {
				pid := binary.LittleEndian.Uint64(b[4+8*i:])
				if loc, ok := locations[pid]; ok && time.Since(lastSeen[pid]) < subscriptionActiveWindow {
					elems = append(elems, loc)
				}
			}
			mu.Unlock()
			out := nex.NewStreamOut(c.Settings)
			out.U32(uint32(len(elems)))
			for _, el := range elems {
				out.Write(el)
			}
			return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
		default:
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreNotImplemented)
		}
	}
}
