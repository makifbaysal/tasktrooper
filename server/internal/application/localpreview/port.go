package localpreview

import (
	"net"
	"strconv"
)

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// portEnv is the port under each name a detected run command reads it from:
// PORT for Next.js, CRA, Express and most Go servers, SERVER_PORT for Spring
// Boot, QUARKUS_HTTP_PORT for Quarkus. Vite reads none of them but moves to
// the next free port on its own.
func portEnv(port int) []string {
	p := strconv.Itoa(port)
	return []string{"PORT=" + p, "SERVER_PORT=" + p, "QUARKUS_HTTP_PORT=" + p}
}
