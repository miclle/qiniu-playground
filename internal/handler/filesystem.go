package handler

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"net/url"
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

func (ctrl *Ctrl) SandboxFilePreview(c *fox.Context) any {
	_, sandboxID, apiKey, endpoint, err := ctrl.sandboxFilesystemContext(c)
	if err != nil {
		return err
	}
	filePath, err := sandboxPreviewPath(c)
	if err != nil {
		return err
	}
	return ctrl.serveSandboxFilePreview(c, sandboxID, apiKey, endpoint, filePath)
}

func (ctrl *Ctrl) WorkspaceFilePreview(c *fox.Context) any {
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	workspaceID := c.Param("workspaceID")
	if workspaceID == "" {
		return httperrors.New(http.StatusBadRequest, "workspace id is required")
	}
	workspace, err := ctrl.service.Workspace(c.Request.Context(), accountID, workspaceID)
	if err != nil {
		return httperrors.New(http.StatusNotFound, "workspace not found")
	}
	if workspace.SandboxID == "" {
		return httperrors.New(http.StatusPreconditionRequired, "workspace sandbox is not connected")
	}
	filePath, err := sandboxPreviewPath(c)
	if err != nil {
		return err
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	return ctrl.serveSandboxFilePreview(c, workspace.SandboxID, apiKey, workspace.Region, filePath)
}

func (ctrl *Ctrl) serveSandboxFilePreview(c *fox.Context, sandboxID, apiKey, endpoint, filePath string) any {
	stream, err := ctrl.sandboxRuntime.ReadFileStream(c.Request.Context(), apiKey, sandboxID, endpoint, filePath)
	if err != nil {
		if isSandboxNotFoundError(err) {
			return httperrors.New(http.StatusConflict, "workspace sandbox no longer exists")
		}
		return err
	}
	defer func() {
		_ = stream.Close()
	}()

	contentType := sandboxPreviewContentType(filePath)
	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(path.Base(filePath)))
	c.Writer.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src * data: blob:; style-src * 'unsafe-inline'; font-src * data:; connect-src 'none'; form-action 'none'; base-uri 'none'")
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(c.Writer, stream)
	return nil
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

func sandboxPreviewPath(c *fox.Context) (string, error) {
	filePath := c.Param("previewPath")
	if filePath == "" {
		return "", httperrors.New(http.StatusBadRequest, "file path is required")
	}
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}
	filePath = path.Clean(filePath)
	if filePath == "/" {
		return "", httperrors.New(http.StatusBadRequest, "file path is required")
	}
	return filePath, nil
}

func sandboxPreviewContentType(filePath string) string {
	extension := strings.ToLower(path.Ext(filePath))
	switch extension {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	}
	if contentType := mime.TypeByExtension(extension); contentType != "" {
		if strings.HasPrefix(contentType, "text/") && !strings.Contains(strings.ToLower(contentType), "charset") {
			return contentType + "; charset=utf-8"
		}
		return contentType
	}
	return "application/octet-stream"
}
