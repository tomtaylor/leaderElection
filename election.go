package election

import (
	"errors"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

func RegisterCallback(callback Callback, multicastNet string, networkInterface string) error {
	participant, err := newParticipant(multicastNet, networkInterface)
	if err != nil {
		return err
	}

	participant.callback = callback
	go participant.watcher()

	return nil
}

func newParticipant(multicastNet string, networkInterface string) (*Participant, error) {
	if !strings.Contains(multicastNet, ":") {
		return nil, errors.New("Muilticast address is not in the format IP:PORT")
	}

	addrsTokens := strings.Split(multicastNet, ":")
	p := &Participant{state: FOLLOWER, ipAddr: addrsTokens[0], port: addrsTokens[1], heardFromLeader: false,
		writeMutex: &sync.Mutex{}, electionTimer: nil, waitForAnotherLeader: false}

	if "" == networkInterface {
		return nil, errors.New("Network interface must be specified")
	}

	var err error
	if p.multicastInterface, err = net.InterfaceByName(networkInterface); err != nil {
		return nil, err
	}

	p.localIpAddr, p.localIpAddrNumeric = getLocalInterfaceIpAddress(p.multicastInterface)

	groupIP := net.ParseIP(addrsTokens[0])
	if !groupIP.IsMulticast() {
		return nil, errors.New("Address supplied is not a multicast address")
	}

	groupIpOctets := strings.Split(addrsTokens[0], ".")
	conn, err := net.ListenPacket("udp4", groupIpOctets[0]+".0.0.0:"+addrsTokens[1])
	if err != nil {
		return nil, err
	}

	p.conn = ipv4.NewPacketConn(conn)
	p.conn.JoinGroup(p.multicastInterface, &net.UDPAddr{IP: groupIP})
	p.conn.SetControlMessage(ipv4.FlagDst, true)
	p.dst, _ = net.ResolveUDPAddr("udp4", multicastNet)

	if err := p.conn.SetMulticastInterface(p.multicastInterface); err != nil {
		return nil, err
	}
	p.conn.SetTTL(MULTICATE_TTL)
	p.pid = os.Getpid()

	return p, nil
}

func (this *Participant) cleanup() {
	this.conn.Close()
}

func (this *Participant) leaderPeriodicAnnouncement() {
	go this.callback(LEADER)
	ticker := time.NewTicker(time.Millisecond * LEADER_PERIODIC_ANNOUNCEMENT_TIME)
	for range ticker.C {
		this.announce("LEADER")
	}
}

func (this *Participant) monitorLeader() {
	bchan := make(chan bool)
	ticker := time.NewTicker(time.Millisecond * LEADER_NOTIFICATION_TIMEOUT)
	exit := false
	for !exit {
		select {
		case <-ticker.C:
			this.Lock()
			if !this.heardFromLeader {
				if this.state == FOLLOWER && !this.waitForAnotherLeader {
					this.state = CANDIDATE
					this.announce("ELECTION")
					this.electionTimer = time.NewTimer(time.Second * ELECTION_TIMEOUT)

					go func() {
						<-this.electionTimer.C
						this.state = LEADER
						ticker.Stop()
						bchan <- true
						go this.leaderPeriodicAnnouncement()
					}()

					go this.callback(CANDIDATE)
				}
			} else {
				this.heardFromLeader = false
			}
			this.Unlock()
		case <-bchan:
			close(bchan)
			exit = true
		}
	}
}

func (this *Participant) watcher() {
	go this.monitorLeader()

	buffer := []byte{}
	readBuf := make([]byte, 1500)
	for {
		num, cm, _, err := this.conn.ReadFrom(readBuf)
		if err != nil {
			log.Println(err)
		}

		if !cm.Dst.IsMulticast() {
			continue
		}

		buffer = append(buffer, readBuf[:num]...)
		for len(buffer) >= MSG_BLOCK_SIZE {
			bytes := buffer[:MSG_BLOCK_SIZE]
			go this.processBytes(bytes)
			buffer = buffer[MSG_BLOCK_SIZE:]
		}
	}
}

func (this *Participant) processBytes(bytes []byte) {
	msg := newMMessageFromBytes(bytes)
	// This is the message we just sent, so ignore it.
	if this.localIpAddrNumeric == msg.ipNumber && this.pid == msg.processId {
		return
	}

	switch strings.ToUpper(msg.message) {
	case "LEADER":
		this.processLeaderRequest(msg)
	case "ELECTION":
		this.processElectionRequest(msg)
	}
}

func (this *Participant) announce(message string) {
	m := &mMessage{
		message:   message,
		ipNumber:  this.localIpAddrNumeric,
		processId: this.pid,
		ipAddr:    this.localIpAddr,
	}
	bytes := m.pack()

	this.writeMutex.Lock()
	if _, err := this.conn.WriteTo(bytes, nil, this.dst); err != nil {
		log.Println(err)
	}
	this.writeMutex.Unlock()
}

func (this *Participant) processLeaderRequest(msg *mMessage) {
	this.Lock()
	this.heardFromLeader = true
	this.Unlock()

	this.waitForAnotherLeader = false
	if this.state == CANDIDATE {
		this.electionTimer.Stop()
		this.state = FOLLOWER
		go this.callback(this.state)
	}
}

func (this *Participant) processElectionRequest(msg *mMessage) {
	if ((this.localIpAddrNumeric == msg.ipNumber) && this.pid < msg.processId) || (this.localIpAddrNumeric < msg.ipNumber) {
		if this.state == CANDIDATE {
			this.electionTimer.Stop()
			this.waitForAnotherLeader = true
			this.state = FOLLOWER
			go this.callback(this.state)

			go func() {
				tmpTimer := time.NewTimer(time.Second * 5)
				<-tmpTimer.C
				this.waitForAnotherLeader = false
			}()
		}
		return
	}

	// At this point we are eligible to become leader
	if this.state == CANDIDATE {
		return
	}
}
