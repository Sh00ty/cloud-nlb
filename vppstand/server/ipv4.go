package server

import (
	"net"

	"github.com/Sh00ty/vppstand"
)

type server struct {
	l net.Listener
}

func ServeIP(network, address string, handle func(c net.Conn), log vppstand.Logger) (*server, error) {
	l, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				log.Errorf("stop accepting connections on %s: %v", network, err)
				return
			}
			log.Infof("accepted connection from %s", c.RemoteAddr())
			go handle(c)
		}
	}()
	return &server{l: l}, nil
}
