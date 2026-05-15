package service

import (
	"bytes"
	"io"
	"strings"
	"time"
)

type CodexGatewayCaptureStreamWriter struct {
	dst       io.Writer
	manager   *CodexGatewayCaptureManager
	trace     *CodexGatewayTrace
	direction string
	buffer    bytes.Buffer
}

func NewCodexGatewayCaptureStreamWriter(dst io.Writer, manager *CodexGatewayCaptureManager, trace *CodexGatewayTrace, direction string) io.Writer {
	if dst == nil || manager == nil || trace == nil {
		return dst
	}
	return &CodexGatewayCaptureStreamWriter{
		dst:       dst,
		manager:   manager,
		trace:     trace,
		direction: direction,
	}
}

func (w *CodexGatewayCaptureStreamWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n > 0 {
		w.observe(p[:n])
	}
	return n, err
}

func (w *CodexGatewayCaptureStreamWriter) observe(p []byte) {
	_, _ = w.buffer.Write(p)
	for {
		raw := w.buffer.String()
		idx := strings.Index(raw, "\n\n")
		if idx < 0 {
			return
		}
		frame := raw[:idx]
		remaining := raw[idx+2:]
		w.buffer.Reset()
		_, _ = w.buffer.WriteString(remaining)
		w.recordFrame(frame)
	}
}

func (w *CodexGatewayCaptureStreamWriter) recordFrame(frame string) {
	frame = strings.TrimRight(frame, "\r\n")
	if strings.TrimSpace(frame) == "" {
		return
	}
	eventName := "message"
	dataLines := make([]string, 0, 2)
	for _, line := range strings.Split(frame, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	payload := []byte(strings.Join(dataLines, "\n"))
	if len(payload) == 0 {
		payload = []byte(frame)
	}
	w.manager.RecordStreamEvent(w.trace, w.direction, eventName, payload)
	if strings.TrimSpace(string(payload)) == "[DONE]" {
		w.manager.RecordStreamEvent(w.trace, w.direction, "done", []byte(`{"done":true}`))
	}
}

func (w *CodexGatewayCaptureStreamWriter) FlushPending() {
	if w == nil || w.buffer.Len() == 0 {
		return
	}
	frame := w.buffer.String()
	w.buffer.Reset()
	w.manager.RecordStreamEvent(w.trace, w.direction, "incomplete_frame", []byte(frame))
	w.manager.RecordStreamEvent(w.trace, w.direction, "capture.flush_pending", []byte(`{"ts":"`+time.Now().UTC().Format(time.RFC3339Nano)+`"}`))
}
