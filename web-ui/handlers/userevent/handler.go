package userevent

import (
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/auth"
)

type Handler struct {
	nats *cs.NATS
}

func RegisterHandler(r *gin.Engine, nats *cs.NATS) {
	if nats == nil {
		return
	}
	h := &Handler{
		nats: nats,
	}
	// auth.HasAuth middleware enforces authentication; no inner check needed.
	r.GET("/user/events", auth.HasAuth, h.events)
}

func (h *Handler) events(c *gin.Context) {
	u := auth.GetUserFromContext(c)

	nc := h.nats.Get()
	if nc == nil {
		c.Status(500)
		return
	}

	// Set up the NATS subscription BEFORE committing to SSE headers,
	// so that a subscription failure can still return a clean HTTP error.
	ch := make(chan *nats.Msg, 16)
	sub, err := nc.ChanSubscribe(fmt.Sprintf("user.%s.update", u.ID.String()), ch)
	if err != nil {
		_ = c.Error(err)
		c.Status(500)
		return
	}
	// Drain flushes any buffered messages and avoids blocking the internal
	// NATS goroutine when the channel is full at disconnect time.
	defer func() {
		// Spawn the channel reader first so it is active during Drain
		go func() {
			for sub.IsValid() {
				select {
				case <-ch:
				case <-time.After(50 * time.Millisecond):
				}
			}
			// Clear any leftover messages in the channel
			for len(ch) > 0 {
				<-ch
			}
		}()
		_ = sub.Drain()
	}()

	// Only set SSE headers once we are committed to streaming.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache,no-store,no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ctx := c.Request.Context()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case msg := <-ch:
			c.SSEvent("message", string(msg.Data))
			return true
		case <-ticker.C:
			c.SSEvent("ping", "")
			return true
		}
	})
}
