package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
)

type sandboxTemplateResponse struct {
	TemplateID  string   `json:"template_id"`
	Aliases     []string `json:"aliases,omitempty"`
	BuildStatus string   `json:"build_status,omitempty"`
	CPUCount    int32    `json:"cpu_count,omitempty"`
	MemoryMB    int32    `json:"memory_mb,omitempty"`
	DiskSizeMB  int32    `json:"disk_size_mb,omitempty"`
	Public      bool     `json:"public"`
	Default     bool     `json:"default"`
}

func (ctrl *Ctrl) SandboxTemplates(c *fox.Context) any {
	region := c.Context.Request.URL.Query().Get("region")
	accountID, err := ctrl.accountIDFromRequest(c)
	if err != nil {
		return httperrors.New(http.StatusUnauthorized, "unauthorized")
	}
	apiKey, err := ctrl.qiniuAPIKey(c, accountID)
	if err != nil {
		return err
	}
	templates, err := ctrl.sandboxRuntime.ListTemplates(c.Request.Context(), apiKey, region)
	if err != nil {
		return err
	}
	out := make([]sandboxTemplateResponse, 0, len(templates))
	for _, template := range templates {
		out = append(out, sandboxTemplateResponse{
			TemplateID:  template.TemplateID,
			Aliases:     template.Aliases,
			BuildStatus: template.BuildStatus,
			CPUCount:    template.CPUCount,
			MemoryMB:    template.MemoryMB,
			DiskSizeMB:  template.DiskSizeMB,
			Public:      template.Public,
			Default:     template.TemplateID == ctrl.defaultSandboxTemplateID,
		})
	}
	return map[string]any{
		"default_template_id": ctrl.defaultSandboxTemplateID,
		"templates":           out,
	}
}
