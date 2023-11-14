package remotelog

import (
	"fmt"
	"net"

	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/logger"
)

// RemoteLoggerConn is a wrapper for a connection to our RemoteLog server.
type RemoteLoggerConn struct {
	conn net.Conn
}

func newRemoteLoggerConn(address string) (*RemoteLoggerConn, error) {
	c, err := net.Dial("udp", address)
	if err != nil {
		return nil, errors.Wrapf(err, "could not create UDP socket to '%s'.", address)
	}

	return &RemoteLoggerConn{conn: c}, nil
}

// SendLogMsg sends log message to the remote logger.
func (r *RemoteLoggerConn) SendLogMsg(level logger.Level, name, msg string) {
	// m := logBlock{
	// 	"v1.0",
	// 	myGitHead,
	// 	myGitConflict,
	// 	myID,
	// 	level.CapitalString(),
	// 	name,
	// 	msg,
	// 	time.Now(),
	// 	remoteLogType,
	// }

	c, err := net.Dial("udp", "192.168.1.122:5213")
	if err != nil {
		fmt.Println(err)
	}

	_, err = c.Write([]byte("hello"))
	Component.LogInfo("send log msg")

	// _ = deps.RemoteLogger.Send(m)
}

// Send sends a message on the RemoteLoggers connection.
func (r *RemoteLoggerConn) Send(msg interface{}) error {
	fmt.Println("enter send log msg")
	remoteLogger, _ := newRemoteLoggerConn("192.168.1.122:5213")
	// c, err := net.Dial("udp", "192.168.1.122:5213")
	// if err != nil {
	// 	fmt.Println(err)
	// }

	// b, err := json.Marshal(msg)
	// if err != nil {
	// 	return err
	// }
	remoteLogger.conn.Write([]byte("hello"))
	fmt.Println("send log msg")
	// fmt.Println(string(b), ParamsRemoteLog.ServerAddress, r.conn.RemoteAddr())
	// _, err = r.conn.Write(b)
	// if err != nil {
	// 	fmt.Println("send remote log failed", err)
	// 	return err
	// }

	return nil
}
