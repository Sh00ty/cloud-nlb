package client

import (
	"fmt"
	"net"
)

type Client struct {
	host    string
	network string
	port    int
}

func (c *Client) Send(data []byte) error {
	ips, err := net.LookupIP(c.host)
	if err != nil {
		return fmt.Errorf("failed to resolve host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %s has no valid IP address", c.host)
	}
	laddr := &net.TCPAddr{}
	net.DialTCP(c.network, laddr, &net.TCPAddr{
		IP:   ips[0],
		Port: c.port,
	})
}
