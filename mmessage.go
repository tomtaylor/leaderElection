package election

import (
	"fmt"
	"strconv"
	"strings"
)

type mMessage struct {
	message   string
	ipNumber  uint32
	processId int
	ipAddr    string
}

func newMMessageFromBytes(bytes []byte) *mMessage {
	data := string(bytes)
	tokens := strings.Split(data, "|")
	num, _ := strconv.ParseUint(tokens[1], 10, 32)
	num2, _ := strconv.ParseInt(tokens[2], 10, 32)
	i := strings.Index(tokens[3], "###")
	ip := tokens[3][:i]

	return &mMessage{message: tokens[0], ipNumber: uint32(num), processId: int(num2), ipAddr: ip}
}

func (m *mMessage) pack() []byte {
	transmitData := fmt.Sprintf("%s|%d|%d|%s", m.message, m.ipNumber, m.processId, m.ipAddr)
	transmitData = transmitData + strings.Repeat("#", MSG_BLOCK_SIZE-len(transmitData))
	return []byte(transmitData)
}
