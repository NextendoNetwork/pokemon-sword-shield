package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const (
	protocolDataStore          uint16 = 0x73
	methodPostUserData         uint32 = 47
	methodGetPersistenceInfos  uint32 = 30
	methodGetUserData          uint32 = 51
	methodGetUserDatas         uint32 = 52
	resultDataStoreNotFound    uint32 = 0x80690004
	resultSuccess              uint32 = 0x00010001
	persistenceInfoContentSize uint32 = 8 + 2 + 8
)

// Sword 1.3.2 Battle Stadium > Rental Teams first calls DataStore method 30, GetPersistenceInfos
// (u64 owner, List<u16> slots) -> (List<DataStorePersistenceInfo>, List<Result>); decoders
// main+0x16a2340 / 0x16a9440 / element 0x16a0330: {u8 version, u32 size, u64 owner, u16 slot,
// u64 dataId}. Nothing is persisted, so each slot is reported with dataId 0 and NotFound.
// Unanswered (no DataStore registered) the game shows 2306-0103.
//
// Casual/Ranked battles call the game's own user-data methods (RequestPostUserData /
// RequestGetUserData, main+0x1199980):
//   47 PostUserData {u8 version, u32 size, u8 type, u8 flag, qBuffer data} -> void (serializer 0x16a5e10)
//   51 GetUserData {u64 pid, u8 type} -> {u64 pid, u8 type, qBuffer data} (0x16a5d20 / 0x16a5b40)
//   52 GetUserDatas List<{u64 pid, u8 type}> -> List<UserData>, List<Result> (0x16aa7a0)
// NotFound (0x80690004) is the game's "no data yet" answer (0x119e2e0 maps it to status 1, which
// the Battle Stadium callback 0xb28e40 treats as success).
func dataStoreHandler() func(*nex.Connection, *nex.RMCMessage) *nex.RMCMessage {
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		switch req.Method {
		case methodGetPersistenceInfos:
			return getPersistenceInfos(c, req)
		case methodPostUserData:
			return postUserData(c, req)
		case methodGetUserData:
			return getUserData(c, req)
		case methodGetUserDatas:
			return getUserDatas(c, req)
		}
		fmt.Printf("[DataStore] pid=%d method=%d not implemented body=%x\n", c.PID, req.Method, req.Body)
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreNotImplemented)
	}
}

func getPersistenceInfos(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	b := req.Body
	if len(b) < 12 {
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
	}
	owner := binary.LittleEndian.Uint64(b[:8])
	n := int(binary.LittleEndian.Uint32(b[8:12]))
	if 12+2*n != len(b) {
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
	}
	out := nex.NewStreamOut(c.Settings)
	out.U32(uint32(n))
	for i := 0; i < n; i++ {
		out.Write([]byte{0})
		out.U32(persistenceInfoContentSize)
		out.U64(owner)
		out.U16(binary.LittleEndian.Uint16(b[12+2*i:]))
		out.U64(0)
	}
	out.U32(uint32(n))
	for i := 0; i < n; i++ {
		out.U32(resultDataStoreNotFound)
	}
	fmt.Printf("[DataStore] pid=%d GetPersistenceInfos owner=%d slots=%d -> none\n", c.PID, owner, n)
	return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
}

type userDataEntry struct {
	Flag byte   `json:"flag"`
	Data []byte `json:"data"`
}

var userData = struct {
	sync.Mutex
	m map[string]userDataEntry
}{m: loadUserData()}

const userDataFile = "userdata.json"

func userDataKey(pid uint64, typ byte) string { return fmt.Sprintf("%d:%d", pid, typ) }

func loadUserData() map[string]userDataEntry {
	m := map[string]userDataEntry{}
	if b, err := os.ReadFile(userDataFile); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// saveUserDataLocked writes the store; the caller holds userData's lock.
func saveUserDataLocked() {
	b, err := json.Marshal(userData.m)
	if err != nil {
		return
	}
	if err := os.WriteFile(userDataFile+".tmp", b, 0o600); err == nil {
		_ = os.Rename(userDataFile+".tmp", userDataFile)
	}
}

// readStructHeader skips a structure's {u8 version, u32 size} header and returns its content.
func readStructHeader(b []byte) ([]byte, []byte, bool) {
	if len(b) < 5 {
		return nil, nil, false
	}
	size := int(binary.LittleEndian.Uint32(b[1:5]))
	if len(b) < 5+size {
		return nil, nil, false
	}
	return b[5 : 5+size], b[5+size:], true
}

// readQBuffer reads a u16-length byte buffer (the client's decoder is main+0x1741ab0).
func readQBuffer(b []byte) ([]byte, bool) {
	if len(b) < 2 {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint16(b))
	if len(b) < 2+n {
		return nil, false
	}
	return append([]byte(nil), b[2:2+n]...), true
}

func postUserData(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	content, _, ok := readStructHeader(req.Body)
	if !ok || len(content) < 2 {
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
	}
	data, ok := readQBuffer(content[2:])
	if !ok {
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
	}
	typ, flag := content[0], content[1]
	userData.Lock()
	userData.m[userDataKey(uint64(c.PID), typ)] = userDataEntry{Flag: flag, Data: data}
	saveUserDataLocked()
	userData.Unlock()
	fmt.Printf("[DataStore] pid=%d PostUserData type=%d flag=%d len=%d\n", c.PID, typ, flag, len(data))
	return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, nil)
}

func lookupUserData(pid uint64, typ byte) (userDataEntry, bool) {
	userData.Lock()
	defer userData.Unlock()
	e, ok := userData.m[userDataKey(pid, typ)]
	return e, ok
}

func writeUserData(out *nex.StreamOut, pid uint64, typ byte, data []byte) {
	content := nex.NewStreamOut(out.Settings)
	content.U64(pid)
	content.U8(typ)
	content.QBuffer(data)
	body := content.Bytes()
	out.Write([]byte{0})
	out.U32(uint32(len(body)))
	out.Write(body)
}

func parseUserDataParam(b []byte) (uint64, byte, []byte, bool) {
	content, rest, ok := readStructHeader(b)
	if !ok || len(content) < 9 {
		return 0, 0, nil, false
	}
	return binary.LittleEndian.Uint64(content), content[8], rest, true
}

func getUserData(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	pid, typ, _, ok := parseUserDataParam(req.Body)
	if !ok {
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
	}
	e, found := lookupUserData(pid, typ)
	if !found {
		fmt.Printf("[DataStore] pid=%d GetUserData owner=%d type=%d -> NotFound\n", c.PID, pid, typ)
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, resultDataStoreNotFound)
	}
	out := nex.NewStreamOut(c.Settings)
	writeUserData(out, pid, typ, e.Data)
	fmt.Printf("[DataStore] pid=%d GetUserData owner=%d type=%d -> len=%d\n", c.PID, pid, typ, len(e.Data))
	return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
}

func getUserDatas(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	b := req.Body
	if len(b) < 4 {
		return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
	}
	n := int(binary.LittleEndian.Uint32(b))
	b = b[4:]
	out := nex.NewStreamOut(c.Settings)
	out.U32(uint32(n))
	results := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		pid, typ, rest, ok := parseUserDataParam(b)
		if !ok {
			return nex.NewRMCError(c.Settings, req.Protocol, req.CallID, nex.ResultCoreInvalidArgument)
		}
		b = rest
		e, found := lookupUserData(pid, typ)
		writeUserData(out, pid, typ, e.Data)
		if found {
			results = append(results, resultSuccess)
		} else {
			results = append(results, resultDataStoreNotFound)
		}
	}
	out.U32(uint32(len(results)))
	for _, r := range results {
		out.U32(r)
	}
	fmt.Printf("[DataStore] pid=%d GetUserDatas n=%d\n", c.PID, n)
	return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, out.Bytes())
}
