package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// MatchmakeReferee (0x78), Sword 1.3.2 client (handler table GOT 0x262e248). Ranked Battles call
// 12 GetOrCreateStats {u32 category, u32 initialRating} -> Stats before matching; the Ranked
// parameter provider (0xadb3c0) refuses to match without a stats uniqueId. Formats from the
// client serializers/decoders:
//   1 StartRound {u32 category, u32 gid, List<u64 pid>} -> u64 roundId (0x16c3de0 / 0x16c3a90)
//   3 EndRound {u64 roundId, List<PersonalRoundResult>} -> void (0x16c36f0)
//   9 GetStatsPrimaries List<{u64 pid, u32 category}> -> List<Stats>, List<Result> (0x16c3b40)
//  12 GetOrCreateStats -> Stats (0x16c4300)
//  14 {u64 pid, u32 category} -> {u32 rank, u8} (0x16c34d0); no standings are kept, so rank 0.
// Stats: {u64 uniqueId, u32 category, u64 pid, u32 x16, u16 rating, u16}. The u32s follow the NEX
// order (recent disconnect/violation/mismatch/win/loss/draw, total disconnect/violation/mismatch/
// win/loss/draw) plus four more; the game sums totals win/loss/draw (0xadb560) and centres
// matching on the u16 rating (1500 start, 0xadb780).
const protocolMatchmakeReferee uint16 = 0x78

type refereeStats struct {
	UniqueID uint64     `json:"id"`
	Category uint32     `json:"category"`
	PID      uint64     `json:"pid"`
	Counters [16]uint32 `json:"counters"`
	Rating   uint16     `json:"rating"`
	Extra    uint16     `json:"extra"`
}

const (
	statTotalWin  = 9
	statTotalLoss = 10
	statTotalDraw = 11
	statRecentWin = 3
)

var referee = struct {
	sync.Mutex
	Stats  map[string]*refereeStats `json:"stats"`
	NextID uint64                   `json:"next_id"`
	rounds map[uint64][]uint64
	cats   map[uint64]uint32
}{Stats: map[string]*refereeStats{}, NextID: 1, rounds: map[uint64][]uint64{}, cats: map[uint64]uint32{}}

const refereeFile = "referee.json"

func init() {
	if b, err := os.ReadFile(refereeFile); err == nil {
		_ = json.Unmarshal(b, &referee)
		if referee.Stats == nil {
			referee.Stats = map[string]*refereeStats{}
		}
		if referee.NextID == 0 {
			referee.NextID = 1
		}
	}
}

func refereeKey(pid uint64, category uint32) string { return fmt.Sprintf("%d:%d", pid, category) }

// saveRefereeLocked writes the stats; the caller holds referee's lock.
func saveRefereeLocked() {
	b, err := json.Marshal(&referee)
	if err != nil {
		return
	}
	if err := os.WriteFile(refereeFile+".tmp", b, 0o600); err == nil {
		_ = os.Rename(refereeFile+".tmp", refereeFile)
	}
}

func getOrCreateStatsLocked(pid uint64, category uint32, rating uint32) *refereeStats {
	k := refereeKey(pid, category)
	if s, ok := referee.Stats[k]; ok {
		return s
	}
	s := &refereeStats{UniqueID: referee.NextID, Category: category, PID: pid, Rating: uint16(rating)}
	referee.NextID++
	referee.Stats[k] = s
	saveRefereeLocked()
	return s
}

func writeRefereeStats(out *nex.StreamOut, s *refereeStats) {
	content := nex.NewStreamOut(out.Settings)
	content.U64(s.UniqueID)
	content.U32(s.Category)
	content.U64(s.PID)
	for _, v := range s.Counters {
		content.U32(v)
	}
	content.U16(s.Rating)
	content.U16(s.Extra)
	body := content.Bytes()
	out.Write([]byte{0})
	out.U32(uint32(len(body)))
	out.Write(body)
}

func refereeHandler() func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		bad := func() *nex.RMCMessage {
			fmt.Printf("[Referee] pid=%d method=%d bad body=%x\n", c.PID, req.Method, req.Body)
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
		}
		out := nex.NewStreamOut(c.Settings)
		switch req.Method {
		case 12:
			content, _, ok := readStructHeader(req.Body)
			if !ok || len(content) < 8 {
				return bad()
			}
			category := binary.LittleEndian.Uint32(content)
			rating := binary.LittleEndian.Uint32(content[4:])
			referee.Lock()
			s := getOrCreateStatsLocked(uint64(c.PID), category, rating)
			writeRefereeStats(out, s)
			referee.Unlock()
			fmt.Printf("[Referee] pid=%d GetOrCreateStats category=%d -> id=%d rating=%d\n", c.PID, category, s.UniqueID, s.Rating)
		case 9:
			b := req.Body
			if len(b) < 4 {
				return bad()
			}
			n := int(binary.LittleEndian.Uint32(b))
			b = b[4:]
			results := make([]uint32, 0, n)
			var found []*refereeStats
			referee.Lock()
			for i := 0; i < n; i++ {
				content, rest, ok := readStructHeader(b)
				if !ok || len(content) < 12 {
					referee.Unlock()
					return bad()
				}
				b = rest
				pid := binary.LittleEndian.Uint64(content)
				category := binary.LittleEndian.Uint32(content[8:])
				found = append(found, getOrCreateStatsLocked(pid, category, 1500))
				results = append(results, resultSuccess)
			}
			out.U32(uint32(len(found)))
			for _, s := range found {
				writeRefereeStats(out, s)
			}
			referee.Unlock()
			out.U32(uint32(len(results)))
			for _, r := range results {
				out.U32(r)
			}
			fmt.Printf("[Referee] pid=%d GetStatsPrimaries n=%d found=%d\n", c.PID, n, len(found))
		case 14:
			content, _, ok := readStructHeader(req.Body)
			if !ok || len(content) < 12 {
				return bad()
			}
			body := binary.LittleEndian.AppendUint32(nil, 0)
			body = append(body, 0)
			out.Write([]byte{0})
			out.U32(uint32(len(body)))
			out.Write(body)
			fmt.Printf("[Referee] pid=%d method 14 pid=%d category=%d -> no standing\n", c.PID,
				binary.LittleEndian.Uint64(content), binary.LittleEndian.Uint32(content[8:]))
		case 1:
			content, _, ok := readStructHeader(req.Body)
			if !ok || len(content) < 12 {
				return bad()
			}
			category := binary.LittleEndian.Uint32(content)
			gid := binary.LittleEndian.Uint32(content[4:])
			n := int(binary.LittleEndian.Uint32(content[8:]))
			if len(content) < 12+8*n {
				return bad()
			}
			pids := make([]uint64, n)
			for i := range pids {
				pids[i] = binary.LittleEndian.Uint64(content[12+8*i:])
			}
			referee.Lock()
			id := referee.NextID
			referee.NextID++
			referee.rounds[id] = pids
			referee.cats[id] = category
			saveRefereeLocked()
			referee.Unlock()
			out.U64(id)
			fmt.Printf("[Referee] pid=%d StartRound category=%d gid=%d pids=%v -> round %d\n", c.PID, category, gid, pids, id)
		case 3:
			content, _, ok := readStructHeader(req.Body)
			if !ok || len(content) < 12 {
				return bad()
			}
			round := binary.LittleEndian.Uint64(content)
			n := int(binary.LittleEndian.Uint32(content[8:]))
			rest := content[12:]
			referee.Lock()
			category := referee.cats[round]
			for i := 0; i < n; i++ {
				el, r, ok := readStructHeader(rest)
				if !ok || len(el) < 20 {
					break
				}
				rest = r
				pid := binary.LittleEndian.Uint64(el)
				winLoss := binary.LittleEndian.Uint32(el[12:])
				change := int32(binary.LittleEndian.Uint32(el[16:]))
				s := getOrCreateStatsLocked(pid, category, 1500)
				switch winLoss {
				case 1:
					s.Counters[statTotalWin]++
					s.Counters[statRecentWin]++
				case 2:
					s.Counters[statTotalLoss]++
				case 3:
					s.Counters[statTotalDraw]++
				}
				r32 := int32(s.Rating) + change
				if r32 < 0 {
					r32 = 0
				}
				s.Rating = uint16(r32)
				fmt.Printf("[Referee] round %d pid=%d result=%d change=%d -> rating %d\n", round, pid, winLoss, change, s.Rating)
			}
			delete(referee.rounds, round)
			delete(referee.cats, round)
			saveRefereeLocked()
			referee.Unlock()
		default:
			fmt.Printf("[Referee] pid=%d method=%d not implemented body=%x\n", c.PID, req.Method, req.Body)
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreNotImplemented)
		}
		return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
	}
}
