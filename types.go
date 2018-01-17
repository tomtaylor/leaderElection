package election

import (
	"golang.org/x/net/ipv4"
	"net"
	"sync"
	"time"
)

type Callback func(state int)

const msgBlockSize = 128
const LEADER_NOTIFICATION_TIMEOUT = 500
const LEADER_PERIODIC_ANNOUNCEMENT_TIME = 250
const ELECTION_TIMEOUT = 2
const MULTICAST_TTL = 10

const (
	FOLLOWER = iota
	CANDIDATE
	LEADER
)

type Participant struct {
	state int
	//multicast ip and port
	ipAddr               string
	port                 string
	dst                  *net.UDPAddr
	callback             Callback
	conn                 *ipv4.PacketConn
	heardFromLeader      bool
	waitForAnotherLeader bool
	sync.Mutex
	writeMutex         *sync.Mutex
	electionTimer      *time.Timer
	multicastInterface *net.Interface

	//local interface addr
	localIPAddr        string
	localIPAddrNumeric uint32
	pid                int
}
