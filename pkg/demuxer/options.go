package demuxer

import (
	"net"
	"time"

	"github.com/muxfd/multipath-udp/pkg/networking"
)

// HandshakeTimeout specifies the duration before resending a UDP handshake.
func HandshakeTimeout(t time.Duration) func(*Demuxer) {
	return func(d *Demuxer) {
		d.handshakeTimeout = t
	}
}

// AutoBindInterfaces adds a network interface listener to automatically
// add or remove network interfaces.
func AutoBindInterfaces() func(*Demuxer) {
	binder := networking.NewAutoBinder(net.Interfaces, 3*time.Second)
	return func(d *Demuxer) {
		close := binder.Bind(d.AddInterface, d.RemoveInterface)
		go func() {
			d.Wait()
			close()
		}()
	}
}
