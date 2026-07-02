package handler

import (
	"context"
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
	pty, err := ctrl.sandboxRuntime.StartPTY(c.Request.Context(), apiKey, sandboxID, session.Region, sandboxPTYSize{Cols: 80, Rows: 24}, func(data []byte) {
		select {
		case output <- data:
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

	stop := make(chan struct{})
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
			close(stop)
			<-done
			return nil
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			close(stop)
			<-done
			return nil
		}
		if err := pty.Send(c.Request.Context(), data); err != nil {
			close(stop)
			<-done
			return err
		}
	}
}
