// ws.go, a minimal RFC 6455 WebSocket CLIENT, standard library only.
//
// # WHY THIS FILE EXISTS AT ALL
//
// The DevTools Protocol is only reachable over a WebSocket, and the Go standard
// library has no WebSocket client. The repository rule (root CLAUDE.md sections
// 6 and 7) is that no dependency arrives without going into the pinned-versions
// table, and a test harness is a poor reason to add one: it would ship in go.mod
// for every consumer of this module forever. So the client is here, and it is
// deliberately the SMALLEST thing that can talk to Chromium:
//
//   - client-to-server text frames, masked, as the RFC requires of a client;
//   - server-to-browser text frames, unmasked, reassembled across continuation
//     frames because a big Runtime.evaluate result arrives fragmented;
//   - ping answered with a pong, close reported as a clean end;
//   - no permessage-deflate: the handshake simply never offers the extension,
//     so Chromium sends plain frames and there is nothing to inflate.
//
// It is NOT a general-purpose WebSocket library and must not grow into one. If
// this ever needs binary frames, subprotocols or compression, that is the signal
// that a real dependency has become the honest answer, and it goes in the pinned
// table with a note.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// wsConn is one open WebSocket connection. It is used by a single goroutine
// (the harness is strictly request/reply) so it carries no locking.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// wsDial opens a WebSocket to a ws:// URL and completes the RFC 6455 handshake.
//
// Only ws:// is supported: the DevTools endpoint is always plain loopback HTTP,
// and a TLS path would be dead code. An https/wss URL is refused with a message
// that says so rather than failing somewhere deep in the handshake.
func wsDial(rawURL string, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("the DevTools WebSocket URL %q could not be parsed: %w", rawURL, err)
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf(
			"this harness speaks ws:// only, but the DevTools URL is %q. "+
				"The DevTools endpoint is plain loopback HTTP, so a wss:// URL means "+
				"something other than a local Chromium answered", rawURL)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "80")
	}

	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, fmt.Errorf("could not connect to the DevTools endpoint at %s: %w", host, err)
	}

	// A per-connection random key, echoed back hashed by the server. We do not
	// verify the echo: this is a loopback connection to a browser we started
	// ourselves, and the handshake's purpose here is protocol compliance, not
	// authentication.
	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		conn.Close()
		return nil, fmt.Errorf("could not generate a WebSocket key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes[:])

	path := u.RequestURI()
	handshake := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := io.WriteString(conn, handshake); err != nil {
		conn.Close()
		return nil, fmt.Errorf("could not send the WebSocket handshake to %s: %w", host, err)
	}

	br := bufio.NewReaderSize(conn, 64*1024)
	// net/http parses the response for us, which is one fewer hand-rolled parser
	// to get wrong. A 101 is the only acceptable answer.
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("the DevTools endpoint at %s gave no readable HTTP response: %w", host, err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf(
			"the DevTools endpoint at %s answered HTTP %d instead of 101 Switching Protocols. "+
				"That usually means the target has gone away: re-read /json/list and use a fresh "+
				"webSocketDebuggerUrl", host, resp.StatusCode)
	}

	// The handshake deadline must not become the read deadline for the whole
	// session; each read sets its own.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, br: br}, nil
}

// writeText sends one unfragmented, masked text frame.
//
// The mask is mandatory for a client (RFC 6455 section 5.3) and Chromium closes
// the connection on an unmasked client frame, so this is not optional politeness.
func (c *wsConn) writeText(payload []byte) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}

	var header []byte
	header = append(header, 0x80|0x1) // FIN + opcode 1 (text)

	n := len(payload)
	switch {
	case n < 126:
		header = append(header, 0x80|byte(n)) // mask bit + 7-bit length
	case n <= 0xFFFF:
		header = append(header, 0x80|126)
		header = binary.BigEndian.AppendUint16(header, uint16(n))
	default:
		header = append(header, 0x80|127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("could not generate a frame mask: %w", err)
	}
	header = append(header, mask[:]...)

	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}

	if _, err := c.conn.Write(append(header, masked...)); err != nil {
		return fmt.Errorf("could not send a DevTools message: %w", err)
	}
	return nil
}

// readText returns the next complete TEXT message, reassembling continuation
// frames. Control frames are handled transparently: a ping is answered, a pong
// ignored, and a close reported as io.EOF so the caller can distinguish "the
// browser hung up" from "the read timed out".
func (c *wsConn) readText(deadline time.Time) ([]byte, error) {
	var message []byte
	for {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case 0x0, 0x1: // continuation, text
			message = append(message, payload...)
			if fin {
				return message, nil
			}
		case 0x2:
			// Chromium never sends binary on the DevTools socket. Saying so beats
			// silently dropping bytes and then failing to parse JSON.
			return nil, fmt.Errorf(
				"the DevTools endpoint sent a binary frame (%d bytes), which this harness "+
					"does not decode. Nothing in the DevTools Protocol should be binary", len(payload))
		case 0x8: // close
			return nil, io.EOF
		case 0x9: // ping
			if err := c.writeControl(0xA, payload); err != nil {
				return nil, err
			}
		case 0xA: // pong, unsolicited
		default:
			return nil, fmt.Errorf("the DevTools endpoint sent an unknown WebSocket opcode 0x%x", opcode)
		}
	}
}

// readFrame reads exactly one frame header plus its payload.
func (c *wsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.br, head[:]); err != nil {
		return false, 0, nil, err
	}
	fin = head[0]&0x80 != 0
	opcode = head[0] & 0x0F

	if head[1]&0x80 != 0 {
		// A server frame must not be masked. If one is, we are not talking to a
		// conforming WebSocket server and every byte after this is guesswork.
		return false, 0, nil, fmt.Errorf(
			"the DevTools endpoint sent a MASKED frame, which a server must never do (RFC 6455 section 5.1)")
	}

	var length uint64
	switch n := head[1] & 0x7F; n {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	default:
		length = uint64(n)
	}

	// A sanity ceiling. The largest legitimate reply here is a document's
	// innerText, tens of kilobytes; 64 MiB means something has gone wrong and
	// allocating for it would take the harness down with an unhelpful OOM.
	const maxMessage = 64 << 20
	if length > maxMessage {
		return false, 0, nil, fmt.Errorf(
			"the DevTools endpoint announced a %d byte frame, over this harness's %d byte ceiling. "+
				"A probe is returning far more data than intended: have it return a summary rather than "+
				"a whole document", length, maxMessage)
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return false, 0, nil, err
	}
	return fin, opcode, payload, nil
}

// writeControl sends a masked control frame (pong, close).
func (c *wsConn) writeControl(opcode byte, payload []byte) error {
	if len(payload) > 125 {
		payload = payload[:125] // control frames are capped by the RFC
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	frame := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := c.conn.Write(frame)
	return err
}

// close says goodbye politely and then drops the socket either way: the browser
// is about to be killed, so a stuck close handshake must not hold the harness up.
func (c *wsConn) close() {
	_ = c.writeControl(0x8, []byte{0x03, 0xE8}) // 1000, normal closure
	_ = c.conn.Close()
}
