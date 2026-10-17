package main

import (
	"encoding/binary"
	"fmt"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// Sword 1.3.2: MatchmakeExtension method 55 is the Battle Stadium rule set query (request builder
// main+0x16b5400, decoder 0x16b8c70 -> 0x16bcad0). The shared stack answers 55 as SSBU's arena
// code, which the game cannot decode, so "Checking the data..." spins forever. The reply is a
// structure {u8 version, u32 size, List<RegulationIndex>, u32} with no Data parent header; no
// official rule sets exist any more, so the list is empty.
//
// Method 60 is SearchCompetition (RequestSearchCompetition, serializer 0x16b7070): {ResultRange,
// List<5-byte>, u8 category, List<u8>}. Category 3 is the Battle Stadium season that Casual and
// Ranked both read (0xb3c370); categories 1/2 are online competitions and get an empty list.
// The reply has the same shape as 55 with competition records (element decoder 0x16b65e0,
// converter 0xb29290):
//   u32 id, u8 type (3 = season), DateTime start, DateTime end, DateTime close, u8, u32 regulation
//   (preset id, 0x1182010), u32, Map<u8,u32> (0 = battle type 0 singles/1 doubles, 0xb3afb8;
//   1 = season number 1..48, 0xb3c6c8), qBuffer regulation blob, u32.
// The blob must be 0x644 bytes, a 0x640-byte regulation plus CRC-16/ARC (0xae0910, 0x8fe850,
// 0x65dd70). Without a Singles and a Doubles season, or with a bad blob, matching fails at once.
func matchmakeExtensionHandler(shared func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage) func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if req.Method == 63 {
			return getRankMatchSetting(c, req)
		}
		if req.Method != 55 && req.Method != 60 {
			return shared(c, req)
		}
		var elems [][]byte
		if req.Method == 60 && searchCompetitionCategory(req.Body) == 3 {
			elems = append(elems,
				nextendoSeasonRecord(1, 0, 1),
				nextendoSeasonRecord(2, 1, 4))
		}
		fmt.Printf("[Regulation] pid=%d method %d body=%x -> %d record(s)\n", c.PID, req.Method, req.Body, len(elems))
		body := binary.LittleEndian.AppendUint32(nil, uint32(len(elems)))
		for _, el := range elems {
			body = append(body, el...)
		}
		body = binary.LittleEndian.AppendUint32(body, 0)
		out := nex.NewStreamOut(c.Settings)
		out.Write([]byte{0})
		out.U32(uint32(len(body)))
		out.Write(body)
		return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
	}
}

// getRankMatchSetting answers method 63 (RequestGetRankMatchSetting, u32 setting id from settings
// keys 0x32/0x33). Reply (decoder 0x16bc170, object 0xae20e0): structure {u32 id,
// List<List<u16>> bands, List<List<u16>> values}. The Ranked parameter provider (0xadb3c0) falls back
// to the settings keys when a band lookup misses (0xadb764), so empty tables are valid.
func getRankMatchSetting(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	if len(req.Body) < 4 {
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
	}
	id := binary.LittleEndian.Uint32(req.Body)
	body := binary.LittleEndian.AppendUint32(nil, id)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	out := nex.NewStreamOut(c.Settings)
	out.Write([]byte{0})
	out.U32(uint32(len(body)))
	out.Write(body)
	fmt.Printf("[Regulation] pid=%d GetRankMatchSetting id=%d -> empty tables\n", c.PID, id)
	return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
}

// searchCompetitionCategory returns the category byte of a SearchCompetition parameter, or -1.
func searchCompetitionCategory(b []byte) int {
	content, _, ok := readStructHeader(b)
	if !ok {
		return -1
	}
	_, rest, ok := readStructHeader(content) // ResultRange
	if !ok || len(rest) < 4 {
		return -1
	}
	n := int(binary.LittleEndian.Uint32(rest))
	off := 4 + 5*n
	if len(rest) <= off {
		return -1
	}
	return int(rest[off])
}

// nexDateTime packs a time the way NEX DateTime does.
func nexDateTime(t time.Time) uint64 {
	t = t.UTC()
	return uint64(t.Second()) | uint64(t.Minute())<<6 | uint64(t.Hour())<<12 |
		uint64(t.Day())<<17 | uint64(t.Month())<<22 | uint64(t.Year())<<26
}

// nextendoSeasonStart anchors the season so every player sees the same dates.
var nextendoSeasonStart = time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

func nextendoSeasonRecord(id uint32, battleType byte, regulation uint32) []byte {
	start := nextendoSeasonStart
	end := start.AddDate(1, 0, 0)
	c := binary.LittleEndian.AppendUint32(nil, id)
	c = append(c, 3)
	// Ranked only matches while battleStart < now < battleEnd (0xb4b510); outside it shows "The season
	// has ended." (mode 0x15 -> msg_ui_btlspot_win_07).
	c = binary.LittleEndian.AppendUint64(c, nexDateTime(start))
	c = binary.LittleEndian.AppendUint64(c, nexDateTime(start)) // battle start
	c = binary.LittleEndian.AppendUint64(c, nexDateTime(end))   // battle end
	c = append(c, 0)
	c = binary.LittleEndian.AppendUint32(c, regulation)
	c = binary.LittleEndian.AppendUint32(c, 0)
	c = binary.LittleEndian.AppendUint32(c, 2) // Map<u8,u32>
	c = append(c, 0)
	c = binary.LittleEndian.AppendUint32(c, uint32(battleType))
	c = append(c, 1)
	c = binary.LittleEndian.AppendUint32(c, 1)
	blob := nextendoRegulationBlob(id)
	c = binary.LittleEndian.AppendUint16(c, uint16(len(blob)))
	c = append(c, blob...)
	c = binary.LittleEndian.AppendUint32(c, 0)
	el := []byte{0}
	el = binary.LittleEndian.AppendUint32(el, uint32(len(c)))
	return append(el, c...)
}

// nextendoRegulationBlob builds the 0x644-byte competition regulation: u32 id at 0 (field 0xc),
// everything else zero, CRC-16/ARC of the first 0x640 bytes at 0x640.
func nextendoRegulationBlob(id uint32) []byte {
	b := make([]byte, 0x644)
	binary.LittleEndian.PutUint32(b, id)
	binary.LittleEndian.PutUint16(b[0x640:], crc16ARC(b[:0x640]))
	return b
}

func crc16ARC(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c ^= uint16(x)
		for i := 0; i < 8; i++ {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
	}
	return c
}
