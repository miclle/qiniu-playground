package handler

import (
	"bytes"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
)

type sandboxFilesResponse struct {
	Entries []sandboxFileEntryResponse `json:"entries"`
}

const maxSandboxFilesystemDepth = 8

type sandboxFileEntryResponse struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	Mode          uint32 `json:"mode,omitempty"`
	Permissions   string `json:"permissions,omitempty"`
	Owner         string `json:"owner,omitempty"`
	Group         string `json:"group,omitempty"`
	ModifiedTime  string `json:"modified_time,omitempty"`
	SymlinkTarget string `json:"symlink_target,omitempty"`
}

func (ctrl *Ctrl) SandboxFiles(c *fox.Context) any {
	accountID, sandboxID, apiKey, endpoint, err := ctrl.sandboxFilesystemContext(c)
	if err != nil {
		return err
	}
	_ = accountID
	filePath, err := sandboxFilesystemPath(c, "")
	if err != nil {
		return err
	}
	depth := uint32(1)
	if value := strings.TrimSpace(c.Request.URL.Query().Get("depth")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil || parsed == 0 {
			return httperrors.New(http.StatusBadRequest, "depth must be a positive integer")
		}
		if parsed > maxSandboxFilesystemDepth {
			return httperrors.New(http.StatusBadRequest, "depth must be 8 or less")
		}
		depth = uint32(parsed)
	}
	entries, err := ctrl.sandboxRuntime.ListFiles(c.Request.Context(), apiKey, sandboxID, endpoint, filePath, depth)
	if err != nil {
		return err
	}
	out := make([]sandboxFileEntryResponse, 0, len(entries))
	for _, entry := range entries {
		item := sandboxFileEntryResponse{
			Name:        entry.Name,
			Type:        entry.Type,
			Path:        entry.Path,
			Size:        entry.Size,
			Mode:        entry.Mode,
			Permissions: entry.Permissions,
			Owner:       entry.Owner,
			Group:       entry.Group,
		}
		if !entry.ModifiedTime.IsZero() {
			item.ModifiedTime = entry.ModifiedTime.Format(http.TimeFormat)
		}
		if entry.SymlinkTarget != nil {
			item.SymlinkTarget = *entry.SymlinkTarget
		}
		out = append(out, item)
	}
	return sandboxFilesResponse{Entries: out}
}

func (ctrl *Ctrl) SandboxFileContent(c *fox.Context) any {
	_, sandboxID, apiKey, endpoint, err := ctrl.sandboxFilesystemContext(c)
	if err != nil {
		return err
	}
	filePath, err := sandboxFilesystemPath(c, "file path is required")
	if err != nil {
		return err
	}
	stream, err := ctrl.sandboxRuntime.ReadFileStream(c.Request.Context(), apiKey, sandboxID, endpoint, filePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = stream.Close()
	}()
	var sniff [512]byte
	n, err := io.ReadFull(stream, sniff[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	contentType := http.DetectContentType(sniff[:n])
	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(path.Base(filePath)))
	c.Writer.WriteHeader(http.StatusOK)
	_, err = io.Copy(c.Writer, io.MultiReader(bytes.NewReader(sniff[:n]), stream))
	return err
}

func (ctrl *Ctrl) sandboxFilesystemContext(c *fox.Context) (accountID, sandboxID, apiKey, endpoint string, err error) {
	accountID, err = ctrl.accountIDFromRequest(c)
	if err != nil {
		err = httperrors.New(http.StatusUnauthorized, "unauthorized")
		return
	}
	sandboxID = c.Param("sandboxID")
	if sandboxID == "" {
		err = httperrors.New(http.StatusBadRequest, "sandbox id is required")
		return
	}
	session, err := ctrl.service.SandboxSession(c.Request.Context(), accountID, sandboxID)
	if err != nil {
		return
	}
	endpoint = session.Region
	apiKey, err = ctrl.qiniuAPIKey(c, accountID)
	return
}

func sandboxFilesystemPath(c *fox.Context, emptyMessage string) (string, error) {
	filePath := strings.TrimSpace(c.Request.URL.Query().Get("path"))
	if filePath == "" {
		if emptyMessage == "" {
			return "/", nil
		}
		return "", httperrors.New(http.StatusBadRequest, emptyMessage)
	}
	if !path.IsAbs(filePath) {
		return "", httperrors.New(http.StatusBadRequest, "path must be absolute")
	}
	return path.Clean(filePath), nil
}
