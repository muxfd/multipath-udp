package demuxer

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/muxfd/multipath-udp/pkg/networking"
)

// Demuxer represents a UDP stream demuxer that demuxes a source over multiple senders.
type Demuxer struct {
	senders  map[string]*Sender
	sessions map[string][]byte

	deduplicator *networking.PacketDeduplicator

	conn *net.UDPConn

	interfaces *InterfaceSet

	handshakeTimeout time.Duration

	done *sync.WaitGroup
}

// NewDemuxer creates a new demuxer.
func NewDemuxer(listen, dial *net.UDPAddr, options ...func(*Demuxer)) *Demuxer {
	conn, err := net.ListenUDP("udp", listen)
	if err != nil {
		panic(err)
	}
	conn.SetReadBuffer(1024 * 1024)
	conn.SetWriteBuffer(1024 * 1024)
	var wg sync.WaitGroup
	d := &Demuxer{
		senders:          make(map[string]*Sender),
		sessions:         make(map[string][]byte),
		deduplicator:     networking.NewPacketDeduplicator(),
		conn:             conn,
		interfaces:       NewInterfaceSet(),
		handshakeTimeout: 1 * time.Second,
		done:             &wg,
	}

	wg.Add(1)

	for _, option := range options {
		option(d)
	}

	go func() {
		defer wg.Done()
		if err != nil {
			panic(err)
		}
		for {
			msg := make([]byte, 2048)
			n, senderAddr, err := conn.ReadFromUDP(msg)
			if err != nil {
				fmt.Printf("error reading %v\n", err)
				break
			}

			session := d.GetSession(senderAddr)

			for _, iface := range d.interfaces.GetAll() {
				key := fmt.Sprintf("%s-%s-%s", hex.EncodeToString(session), iface, dial)
				sender, ok := d.senders[key]
				if !ok {
					fmt.Printf("new sender over %v with handshake %v\n", iface, session)
					sender = NewSender(session, iface, dial, func(msg []byte) {
						// if !d.deduplicator.Receive(hex.EncodeToString(session), msg[:n]) {
						conn.WriteToUDP(msg, senderAddr)
						// }
					}, d.handshakeTimeout)
					d.senders[key] = sender
				}
				sender.Write(msg[:n])
			}
		}
	}()

	return d
}

func (d *Demuxer) GetSession(addr *net.UDPAddr) []byte {
	if session, ok := d.sessions[addr.String()]; ok {
		return session
	}
	token := make([]byte, 64)
	_, err := rand.Read(token)
	if err != nil {
		fmt.Printf("failed to generate random bytes: %v\n", err)
	}
	d.sessions[addr.String()] = token
	return token
}

// Wait waits for the demuxer to exit.
func (d *Demuxer) Wait() {
	d.done.Wait()
}

// Close closes all receivers and sinks associated with the muxer, freeing up resources.
func (d *Demuxer) Close() {
	for _, sender := range d.senders {
		sender.Close()
	}
	d.conn.Close()
}
