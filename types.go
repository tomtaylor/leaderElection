package election

import (
	"golang.org/x/net/ipv4"
	"net"
	"sync"
	"time"
)

type Callback func(state int)

const msgBlockSize = 128
const leaderNotificationTimeout = 500
const leaderPeriodicAnnouncementTime = 250
const electionTimeout = 2
const multicastTTL = 10

const (
	Follower = iota
	Candidate
	Leader
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
