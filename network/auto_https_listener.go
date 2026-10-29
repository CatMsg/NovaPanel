package network

import "net"

type AutoHttpsListener struct {
	net.Listener
	allowedHosts []string
}

func NewAutoHttpsListener(listener net.Listener, allowedHosts ...string) net.Listener {
	return &AutoHttpsListener{
		Listener:     listener,
		allowedHosts: append([]string(nil), allowedHosts...),
	}
}

func (l *AutoHttpsListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return NewAutoHttpsConn(conn, l.allowedHosts...), nil
}
