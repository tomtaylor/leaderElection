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

	p.localIPAddr, p.localIPAddrNumeric = getLocalInterfaceIPAddress(p.multicastInterface)

	groupIP := net.ParseIP(addrsTokens[0])
	if !groupIP.IsMulticast() {
		return nil, errors.New("Address supplied is not a multicast address")
	}

	groupIPOctets := strings.Split(addrsTokens[0], ".")
	conn, err := net.ListenPacket("udp4", groupIPOctets[0]+".0.0.0:"+addrsTokens[1])
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
	p.conn.SetTTL(MULTICAST_TTL)
	p.pid = os.Getpid()

	return p, nil
}

func (p *Participant) leaderPeriodicAnnouncement() {
	go p.callback(LEADER)
	ticker := time.NewTicker(time.Millisecond * LEADER_PERIODIC_ANNOUNCEMENT_TIME)
	for range ticker.C {
		p.announce("LEADER")
	}
}

func (p *Participant) monitorLeader() {
	bchan := make(chan bool)
	ticker := time.NewTicker(time.Millisecond * LEADER_NOTIFICATION_TIMEOUT)
	exit := false
	for !exit {
		select {
		case <-ticker.C:
			p.Lock()
			if !p.heardFromLeader {
				if p.state == FOLLOWER && !p.waitForAnotherLeader {
					p.state = CANDIDATE
					p.announce("ELECTION")
					p.electionTimer = time.NewTimer(time.Second * ELECTION_TIMEOUT)

					go func() {
						<-p.electionTimer.C
						p.state = LEADER
						ticker.Stop()
						bchan <- true
						go p.leaderPeriodicAnnouncement()
					}()

					go p.callback(CANDIDATE)
				}
			} else {
				p.heardFromLeader = false
			}
			p.Unlock()
		case <-bchan:
			close(bchan)
			exit = true
		}
	}
}

func (p *Participant) watcher() {
	go p.monitorLeader()

	buffer := []byte{}
	readBuf := make([]byte, 1500)
	for {
		num, cm, _, err := p.conn.ReadFrom(readBuf)
		if err != nil {
			log.Println(err)
		}

		if !cm.Dst.IsMulticast() {
			continue
		}

		buffer = append(buffer, readBuf[:num]...)
		for len(buffer) >= MSG_BLOCK_SIZE {
			bytes := buffer[:MSG_BLOCK_SIZE]
			go p.processBytes(bytes)
			buffer = buffer[MSG_BLOCK_SIZE:]
		}
	}
}

func (p *Participant) processBytes(bytes []byte) {
	msg := newMMessageFromBytes(bytes)
	// p is the message we just sent, so ignore it.
	if p.localIPAddrNumeric == msg.ipNumber && p.pid == msg.processID {
		return
	}

	switch strings.ToUpper(msg.message) {
	case "LEADER":
		p.processLeaderRequest(msg)
	case "ELECTION":
		p.processElectionRequest(msg)
	}
}

func (p *Participant) announce(message string) {
	m := &mMessage{
		message:   message,
		ipNumber:  p.localIPAddrNumeric,
		processID: p.pid,
		ipAddr:    p.localIPAddr,
	}
	bytes := m.pack()

	p.writeMutex.Lock()
	if _, err := p.conn.WriteTo(bytes, nil, p.dst); err != nil {
		log.Println(err)
	}
	p.writeMutex.Unlock()
}

func (p *Participant) processLeaderRequest(msg *mMessage) {
	p.Lock()
	p.heardFromLeader = true
	p.Unlock()

	p.waitForAnotherLeader = false
	if p.state == CANDIDATE {
		p.electionTimer.Stop()
		p.state = FOLLOWER
		go p.callback(p.state)
	}
}

func (p *Participant) processElectionRequest(msg *mMessage) {
	if ((p.localIPAddrNumeric == msg.ipNumber) && p.pid < msg.processID) || (p.localIPAddrNumeric < msg.ipNumber) {
		if p.state == CANDIDATE {
			p.electionTimer.Stop()
			p.waitForAnotherLeader = true
			p.state = FOLLOWER
			go p.callback(p.state)

			go func() {
				tmpTimer := time.NewTimer(time.Second * 5)
				<-tmpTimer.C
				p.waitForAnotherLeader = false
			}()
		}
		return
	}

	// At p point we are eligible to become leader
	if p.state == CANDIDATE {
		return
	}
}
