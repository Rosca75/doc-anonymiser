// cdp.go, the DevTools Protocol client: request/reply over the WebSocket in
// ws.go, plus the console-and-exception recorder.
//
// The protocol is simple enough not to need a library. Every request is
// {"id":N,"method":...,"params":...} and the reply carries the same id back.
// Unsolicited EVENTS arrive interleaved with replies on the same socket, so the
// read loop dispatches anything that is not the reply we are waiting for.
//
// Two of those events are assertions in their own right: Runtime.consoleAPICalled
// with type "error" and Runtime.exceptionThrown. "No console error during the
// run" is one of the checks this layer owes (docs/UITESTING.md), and it can only
// be collected as it happens, so the recorder is part of the transport rather
// than a probe.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// cdpClient is one attached page target.
type cdpClient struct {
	ws     *wsConn
	nextID int

	// consoleErrors and pageExceptions accumulate for the whole run and are
	// asserted at the end. They are kept verbatim; the filtering of the expected
	// bridge failures happens in checks.go, where the reason for the exemption can
	// be explained next to it.
	consoleErrors  []string
	pageExceptions []string
}

// cdpConnect attaches to a page target's webSocketDebuggerUrl and turns on the
// two domains this harness needs.
func cdpConnect(wsURL string) (*cdpClient, error) {
	ws, err := wsDial(wsURL, 15*time.Second)
	if err != nil {
		return nil, err
	}
	c := &cdpClient{ws: ws, nextID: 1}

	// Runtime.enable is what starts the console and exception events flowing, and
	// it must come before the page finishes booting for the recorder to see the
	// startup errors. Page.enable gives us lifecycle events and the screenshot
	// command.
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if _, err := c.send(method, nil); err != nil {
			ws.close()
			return nil, err
		}
	}

	// Pin the viewport. --window-size is only a request: the headless window
	// subtracts its own chrome, so asking for 1440x900 produced a 1440x708
	// viewport here, and a layout contract measured against an unknown height is
	// not a contract. setDeviceMetricsOverride sets the LAYOUT viewport exactly,
	// so the same numbers come out on a laptop and on a CI runner.
	if _, err := c.send("Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             viewportWidth,
		"height":            viewportHeight,
		"deviceScaleFactor": 1,
		"mobile":            false,
	}); err != nil {
		ws.close()
		return nil, fmt.Errorf("could not pin the viewport to %dx%d: %w", viewportWidth, viewportHeight, err)
	}
	return c, nil
}

// The window the layout contract is measured against. 1440x900 is the size the
// Windows harness uses, so a failure means the same thing on both.
const (
	viewportWidth  = 1440
	viewportHeight = 900
)

func (c *cdpClient) close() { c.ws.close() }

// send issues one command and returns its "result" object.
//
// It reads until the reply with OUR id arrives, handing everything else to
// record(). That is why there is no separate event pump: the harness is always
// either inside a send or about to be, and drain() covers the gaps.
func (c *cdpClient) send(method string, params map[string]any) (json.RawMessage, error) {
	id := c.nextID
	c.nextID++

	if params == nil {
		params = map[string]any{}
	}
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, fmt.Errorf("could not encode the DevTools command %s: %w", method, err)
	}
	if err := c.ws.writeText(payload); err != nil {
		return nil, fmt.Errorf("could not send the DevTools command %s: %w", method, err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		raw, err := c.ws.readText(deadline)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf(
					"the browser closed the DevTools connection while waiting for %s. "+
						"It most likely crashed: re-run with -keep-artifacts and read the browser log", method)
			}
			return nil, fmt.Errorf("no answer to the DevTools command %s: %w", method, err)
		}

		var envelope struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("the DevTools endpoint sent unparseable JSON: %w", err)
		}

		if envelope.Method != "" {
			c.record(envelope.Method, envelope.Params)
			continue
		}
		if envelope.ID != id {
			continue // a reply to something we already gave up on
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("DevTools rejected %s: %s (code %d)",
				method, envelope.Error.Message, envelope.Error.Code)
		}
		return envelope.Result, nil
	}
}

// drain reads whatever events are already queued, for up to d, so console output
// produced after the last command still gets recorded. It is expected to end in a
// timeout; that is the normal, quiet case and not an error.
func (c *cdpClient) drain(d time.Duration) {
	deadline := time.Now().Add(d)
	for {
		raw, err := c.ws.readText(deadline)
		if err != nil {
			return
		}
		var envelope struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &envelope) == nil && envelope.Method != "" {
			c.record(envelope.Method, envelope.Params)
		}
	}
}

// record turns the two interesting events into readable one-line strings.
func (c *cdpClient) record(method string, params json.RawMessage) {
	switch method {
	case "Runtime.consoleAPICalled":
		var ev struct {
			Type string `json:"type"`
			Args []struct {
				Type        string `json:"type"`
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(params, &ev) != nil || ev.Type != "error" {
			return
		}
		parts := make([]string, 0, len(ev.Args))
		for _, a := range ev.Args {
			switch {
			case a.Description != "":
				parts = append(parts, a.Description)
			case a.Value != nil:
				parts = append(parts, fmt.Sprint(a.Value))
			default:
				parts = append(parts, "<"+a.Type+">")
			}
		}
		c.consoleErrors = append(c.consoleErrors, strings.Join(parts, " "))

	case "Runtime.exceptionThrown":
		var ev struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(params, &ev) != nil {
			return
		}
		text := ev.ExceptionDetails.Exception.Description
		if text == "" {
			text = ev.ExceptionDetails.Text
		}
		c.pageExceptions = append(c.pageExceptions, firstLine(text))
	}
}

// eval runs one JavaScript expression in the page and decodes its value into out.
//
// awaitPromise so a probe can be an async function and still be a one-liner here;
// returnByValue so what comes back is plain JSON rather than a remote object
// handle this harness would then have to walk.
func (c *cdpClient) eval(expression string, out any) error {
	result, err := c.send("Runtime.evaluate", map[string]any{
		"expression":    expression,
		"awaitPromise":  true,
		"returnByValue": true,
	})
	if err != nil {
		return err
	}

	var reply struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &reply); err != nil {
		return fmt.Errorf("could not decode the Runtime.evaluate reply: %w", err)
	}
	if reply.ExceptionDetails != nil {
		detail := reply.ExceptionDetails.Exception.Description
		if detail == "" {
			detail = reply.ExceptionDetails.Text
		}
		return fmt.Errorf("the page threw while evaluating %s: %s",
			shorten(expression, 80), firstLine(detail))
	}
	if out == nil {
		return nil
	}
	if len(reply.Result.Value) == 0 {
		return fmt.Errorf("%s returned undefined, so there was nothing to assert on",
			shorten(expression, 80))
	}
	if err := json.Unmarshal(reply.Result.Value, out); err != nil {
		return fmt.Errorf("the value %s returned did not have the shape this harness expects: %w\nvalue: %s",
			shorten(expression, 80), err, shorten(string(reply.Result.Value), 400))
	}
	return nil
}

// screenshot captures the viewport as PNG bytes.
func (c *cdpClient) screenshot() ([]byte, error) {
	result, err := c.send("Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return nil, err
	}
	var reply struct {
		Data []byte `json:"data"` // encoding/json base64-decodes into []byte
	}
	if err := json.Unmarshal(result, &reply); err != nil {
		return nil, fmt.Errorf("could not decode the screenshot: %w", err)
	}
	return reply.Data, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
