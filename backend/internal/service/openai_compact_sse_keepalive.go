package service

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const openAICompactSSEKeepaliveKey = "openai_compact_sse_keepalive"

type openAICompactSSEKeepalive struct {
	mu      sync.Mutex
	writer  gin.ResponseWriter
	started bool
	stopped bool
	bytes   int
	stop    chan struct{}
}

func StartOpenAICompactSSEKeepalive(c *gin.Context, interval time.Duration) func() {
	if c == nil || c.Writer == nil || interval <= 0 || !openAICompactClientWantsStream(c) {
		return func() {}
	}
	originalWriter := c.Writer
	keepalive := &openAICompactSSEKeepalive{
		writer: originalWriter,
		stop:   make(chan struct{}),
	}
	c.Set(openAICompactSSEKeepaliveKey, keepalive)
	installedWriter := &openAICompactKeepaliveWriter{ResponseWriter: originalWriter, keepalive: keepalive}
	c.Writer = installedWriter

	var requestDone <-chan struct{}
	if c.Request != nil {
		requestDone = c.Request.Context().Done()
	}
	go func() {
		timer := time.NewTimer(openAICompactSSEKeepaliveFirstBeatDelay(interval))
		defer timer.Stop()
		for {
			select {
			case <-keepalive.stop:
				return
			case <-requestDone:
				return
			case <-timer.C:
			}
			if !keepalive.beat() {
				return
			}
			timer.Reset(interval)
		}
	}()
	var cleanupOnce sync.Once
	return func() {
		cleanupOnce.Do(func() {
			keepalive.Stop()
			if c.Writer == installedWriter {
				c.Writer = originalWriter
			}
		})
	}
}

func openAICompactSSEKeepaliveFirstBeatDelay(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	// Give routing and fast upstream failures a chance to retain their real HTTP
	// status before the first SSE comment commits a 200 response. Subsequent beats
	// still use the configured interval for long-running compact requests.
	delay := 2 * interval
	if delay > 20*time.Second {
		return 20 * time.Second
	}
	return delay
}

func (k *openAICompactSSEKeepalive) beat() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stopped {
		return false
	}
	if !k.started {
		header := k.writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		k.writer.WriteHeader(http.StatusOK)
		k.started = true
	}
	written, err := k.writer.Write([]byte(": keepalive\n\n"))
	k.bytes += written
	if err != nil {
		k.stopped = true
		return false
	}
	k.writer.Flush()
	return true
}

func (k *openAICompactSSEKeepalive) Stop() {
	k.mu.Lock()
	k.markStoppedLocked()
	k.mu.Unlock()
}

func (k *openAICompactSSEKeepalive) markStoppedLocked() {
	if k.stopped {
		return
	}
	k.stopped = true
	close(k.stop)
}

func StopOpenAICompactSSEKeepaliveCommitted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(openAICompactSSEKeepaliveKey)
	if !ok {
		return false
	}
	keepalive, ok := value.(*openAICompactSSEKeepalive)
	if !ok || keepalive == nil {
		return false
	}
	keepalive.mu.Lock()
	keepalive.markStoppedLocked()
	committed := keepalive.started
	keepalive.mu.Unlock()
	return committed
}

func OpenAICompactKeepaliveAdjustedWrittenSize(c *gin.Context) int {
	if c == nil || c.Writer == nil {
		return -1
	}
	value, ok := c.Get(openAICompactSSEKeepaliveKey)
	if !ok {
		return c.Writer.Size()
	}
	keepalive, ok := value.(*openAICompactSSEKeepalive)
	if !ok || keepalive == nil {
		return c.Writer.Size()
	}
	keepalive.mu.Lock()
	defer keepalive.mu.Unlock()
	size := keepalive.writer.Size()
	if size < 0 {
		return size
	}
	if realSize := size - keepalive.bytes; realSize > 0 {
		return realSize
	}
	return -1
}

type openAICompactKeepaliveWriter struct {
	gin.ResponseWriter
	keepalive *openAICompactSSEKeepalive
}

func (w *openAICompactKeepaliveWriter) suspend() {
	w.keepalive.Stop()
}

func (w *openAICompactKeepaliveWriter) Header() http.Header {
	w.suspend()
	return w.ResponseWriter.Header()
}

func (w *openAICompactKeepaliveWriter) Write(data []byte) (int, error) {
	w.suspend()
	return w.ResponseWriter.Write(data)
}

func (w *openAICompactKeepaliveWriter) WriteString(value string) (int, error) {
	w.suspend()
	return w.ResponseWriter.WriteString(value)
}

func (w *openAICompactKeepaliveWriter) WriteHeader(code int) {
	w.suspend()
	w.ResponseWriter.WriteHeader(code)
}

func (w *openAICompactKeepaliveWriter) WriteHeaderNow() {
	w.suspend()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *openAICompactKeepaliveWriter) Flush() {
	w.suspend()
	w.ResponseWriter.Flush()
}

func (w *openAICompactKeepaliveWriter) Status() int {
	if w == nil || w.keepalive == nil || w.ResponseWriter == nil {
		return http.StatusOK
	}
	w.keepalive.mu.Lock()
	defer w.keepalive.mu.Unlock()
	return w.ResponseWriter.Status()
}

func (w *openAICompactKeepaliveWriter) Size() int {
	if w == nil || w.keepalive == nil || w.ResponseWriter == nil {
		return -1
	}
	w.keepalive.mu.Lock()
	defer w.keepalive.mu.Unlock()
	return w.ResponseWriter.Size()
}

func (w *openAICompactKeepaliveWriter) Written() bool {
	if w == nil || w.keepalive == nil || w.ResponseWriter == nil {
		return false
	}
	w.keepalive.mu.Lock()
	defer w.keepalive.mu.Unlock()
	return w.ResponseWriter.Written()
}
