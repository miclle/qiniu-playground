package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
	"github.com/gorilla/websocket"
)

var ptyUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		originURL, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(originURL.Host, r.Host)
	},
}

type sandboxPTYClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint32 `json:"cols,omitempty"`
	Rows uint32 `json:"rows,omitempty"`
}

func (ctrl *Ctrl) SandboxPTY(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	sandboxID := c.Param("sandboxID")
	if sandboxID == "" {
		return httperrors.New(http.StatusBadRequest, "sandbox id is required")
	}
	session, err := ctrl.service.SandboxSession(c.Request.Context(), accountID, sandboxID)
	if err != nil {
		return httperrors.New(http.StatusNotFound, "sandbox session not found")
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	conn, err := ptyUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	output := make(chan []byte, 32)
	stop := make(chan struct{})
	stopOutput := func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
	pty, err := ctrl.sandboxRuntime.StartPTY(c.Request.Context(), apiKey, sandboxID, session.Region, sandboxPTYSize{Cols: 80, Rows: 24}, func(data []byte) {
		select {
		case output <- data:
		case <-stop:
		case <-c.Request.Context().Done():
		}
	})
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pty.Close(cleanupCtx)
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case data := <-output:
				if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}
		}
	}()

	for {
		messageType, reader, err := conn.NextReader()
		if err != nil {
			stopOutput()
			<-done
			return nil
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			stopOutput()
			<-done
			return nil
		}
		if err := handlePTYClientMessage(c.Request.Context(), pty, messageType, data); err != nil {
			stopOutput()
			<-done
			return err
		}
	}
}

func handlePTYClientMessage(ctx context.Context, pty sandboxPTYSession, messageType int, data []byte) error {
	if messageType == websocket.BinaryMessage {
		return pty.Send(ctx, data)
	}

	var message sandboxPTYClientMessage
	if err := json.Unmarshal(data, &message); err == nil && message.Type != "" {
		switch message.Type {
		case "input":
			return pty.Send(ctx, []byte(message.Data))
		case "resize":
			if message.Cols == 0 || message.Rows == 0 {
				return nil
			}
			return pty.Resize(ctx, sandboxPTYSize{Cols: message.Cols, Rows: message.Rows})
		}
	}

	return pty.Send(ctx, data)
}
