package phpfpm

import (
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Create an fcgi client
func newFcgiClient(timeout time.Duration, h string, args ...interface{}) (*conn, error) {
	var con net.Conn
	if len(args) != 1 {
		return nil, errors.New("fcgi: not enough params")
	}

	var err error
	switch args[0].(type) {
	case int:
		addr := h + ":" + strconv.FormatInt(int64(args[0].(int)), 10)
		if timeout == 0 {
			con, err = net.Dial("tcp", addr)
		} else {
			con, err = net.DialTimeout("tcp", addr, timeout)
		}
	case string:
		laddr := net.UnixAddr{Name: args[0].(string), Net: h}
		con, err = net.DialUnix(h, nil, &laddr)
	default:
		return nil, errors.New("fcgi: we only accept int (port) or string (socket) params")
	}
	if err != nil {
		return nil, err
	}

	if timeout != 0 {
		if err := con.SetDeadline(time.Now().Add(timeout)); err != nil {
			return nil, err
		}
	}

	return &conn{rwc: con}, nil
}

func (c *conn) request(env map[string]string, requestData string) (retout, reterr []byte, err error) {
	defer c.rwc.Close()
	var reqID uint16 = 1

	err = c.writeBeginRequest(reqID, uint16(roleResponder), 0)
	if err != nil {
		return nil, nil, err
	}

	err = c.writePairs(typeParams, reqID, env)
	if err != nil {
		return nil, nil, err
	}

	if len(requestData) > 0 {
		if err := c.writeRecord(typeStdin, reqID, []byte(requestData)); err != nil {
			return nil, nil, err
		}
	}

	rec := &record{}
	var err1 error

	// receive until EOF or FCGI_END_REQUEST
READ_LOOP:
	for {
		err1 = rec.read(c.rwc)
		if err1 != nil && strings.Contains(err1.Error(), "use of closed network connection") {
			if !errors.Is(err1, io.EOF) {
				err = err1
			}
			break
		}
		if err1 != nil && strings.Contains(err1.Error(), "i/o timeout") {
			if !errors.Is(err1, io.EOF) {
				err = err1
			}
			break
		}

		switch {
		case rec.h.Type == typeStdout:
			retout = append(retout, rec.content()...)
		case rec.h.Type == typeStderr:
			reterr = append(reterr, rec.content()...)
		case rec.h.Type == typeEndRequest:
			fallthrough
		default:
			break READ_LOOP
		}
	}

	return retout, reterr, err
}
