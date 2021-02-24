package muxer

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"sync"

	"github.com/muxfd/multipath-udp/pkg/networking"
)

type Muxer struct {
	sessions *AddrToSessionMap
	sinks    map[string]*Sink

	deduplicator *networking.PacketDeduplicator

	conn *net.UDPConn
	done *sync.WaitGroup
}

// NewMuxer creates a new multiplexed listener muxer
func NewMuxer(listen, dial *net.UDPAddr, options ...func(*Muxer)) *Muxer {
	conn, err := net.ListenUDP("udp", listen)
	if err != nil {
		panic(err)
	}
	conn.SetReadBuffer(1024 * 1024)
	conn.SetWriteBuffer(1024 * 1024)
	var wg sync.WaitGroup
	m := &Muxer{
		sessions:     NewAddrToSessionMap(),
		sinks:        make(map[string]*Sink),
		deduplicator: networking.NewPacketDeduplicator(),
		conn:         conn,
		done:         &wg,
	}
	wg.Add(1)

	for _, option := range options {
		option(m)
	}

	go func() {
		defer wg.Done()

		for {
			msg := make([]byte, 2048)

			n, senderAddr, err := conn.ReadFromUDP(msg)
			if err != nil {
				fmt.Printf("error reading %v\n", err)
				break
			}

			session, ok := m.sessions.GetSession(senderAddr)
			if !ok {
				// this is a new session, so the inbound message is the session id.
				fmt.Printf("new session from %v with handshake %v\n", senderAddr, msg[:n])
				session = msg[:n]
				m.sessions.Set(senderAddr, session)
				_, err := conn.WriteToUDP(session, senderAddr)
				if err != nil {
					fmt.Printf("error writing handshake response %v", err)
				}
				// prevent it from being written to the sink.
				m.deduplicator.Receive(hex.EncodeToString(session), msg[:n])
				continue
			} else if bytes.Equal(msg[:n], session) {
				// duplicate handshake, respond until it's successful.
				fmt.Printf("duplicate handshake received\n")
				_, err := conn.WriteToUDP(session, senderAddr)
				if err != nil {
					fmt.Printf("error writing handshake response %v", err)
				}
				continue
			}

			// forward this message to the sink for the session.
			key := hex.EncodeToString(session)
			if !m.deduplicator.Receive(key, msg[:n]) {
				fmt.Printf("duplicate packet\n")
			}
			if true {
				sink, ok := m.sinks[key]
				if !ok {
					sink = NewSink(dial, func(msg []byte) {
						// forward this message to all senders with the same session id as this one.
						for _, sender := range m.sessions.GetUDPAddrs(session) {
							conn.WriteToUDP(msg, sender)
						}
					})
					m.sinks[key] = sink
				}
				sink.Write(msg[:n])
			}
		}
	}()
	return m
}

// Wait for the muxer to terminate.
func (m *Muxer) Wait() {
	m.done.Wait()
}

// Close closes all receivers and sinks associated with the muxer, freeing up resources.
func (m *Muxer) Close() {
	for _, sink := range m.sinks {
		sink.Close()
	}
	m.conn.Close()
}
