// Package entity provides data models and domain types.
package entity

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SandboxMetadata stores sandbox key-value metadata as JSON.
type SandboxMetadata map[string]string

// Value serializes metadata for database writes.
func (m SandboxMetadata) Value() (driver.Value, error) {
	if len(m) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(map[string]string(m))
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

// Scan deserializes metadata from database reads.
func (m *SandboxMetadata) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("scan sandbox metadata: unsupported type %T", value)
	}
	if len(data) == 0 {
		*m = nil
		return nil
	}
	return json.Unmarshal(data, m)
}

// Account is the application-owned user record.
type Account struct {
	ID        string         `gorm:"column:id;primaryKey;size:64" json:"id"`
	CreatedAt time.Time      `gorm:"column:created_at"            json:"created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at"            json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"      json:"deleted_at,omitempty"`
	Name      string         `gorm:"column:name;size:255"         json:"name"`
	AvatarURL string         `gorm:"column:avatar_url;size:1024"  json:"avatar_url"`
}

// TableName returns the database table name for Account.
func (Account) TableName() string {
	return "accounts"
}

// OAuthIdentity binds an external provider subject to a local account.
type OAuthIdentity struct {
	ID              string         `gorm:"column:id;primaryKey;size:64"                                                     json:"id"`
	CreatedAt       time.Time      `gorm:"column:created_at"                                                                json:"created_at"`
	UpdatedAt       time.Time      `gorm:"column:updated_at"                                                                json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"column:deleted_at;index"                                                          json:"deleted_at,omitempty"`
	AccountID       string         `gorm:"column:account_id;size:64;not null;index"                                         json:"account_id"`
	Provider        string         `gorm:"column:provider;size:32;not null;uniqueIndex:idx_oauth_provider_subject"          json:"provider"`
	ProviderSubject string         `gorm:"column:provider_subject;size:255;not null;uniqueIndex:idx_oauth_provider_subject" json:"provider_subject"`
	Login           string         `gorm:"column:login;size:255;not null"                                                   json:"login"`
	DisplayName     string         `gorm:"column:display_name;size:255"                                                     json:"display_name"`
	AvatarURL       string         `gorm:"column:avatar_url;size:1024"                                                      json:"avatar_url"`
	Email           string         `gorm:"column:email;size:320"                                                            json:"email"`
	EncryptedToken  string         `gorm:"column:encrypted_token;type:text"                                                 json:"-"`
	Account         Account        `gorm:"foreignKey:AccountID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"                json:"-"`
}

// TableName returns the database table name for OAuthIdentity.
func (OAuthIdentity) TableName() string {
	return "oauth_identities"
}

// GitHubInstallation records a GitHub App installation connected by a user.
type GitHubInstallation struct {
	ID                  string         `gorm:"column:id;primaryKey;size:64"                                                  json:"id"`
	CreatedAt           time.Time      `gorm:"column:created_at"                                                             json:"created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"                                                             json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"column:deleted_at;index"                                                       json:"deleted_at,omitempty"`
	AccountID           string         `gorm:"column:account_id;size:64;not null;index;uniqueIndex:idx_account_installation" json:"account_id"`
	InstallationID      int64          `gorm:"column:installation_id;not null;uniqueIndex:idx_account_installation"          json:"installation_id"`
	TargetType          string         `gorm:"column:target_type;size:32"                                                    json:"target_type"`
	TargetLogin         string         `gorm:"column:target_login;size:255"                                                  json:"target_login"`
	RepositorySelection string         `gorm:"column:repository_selection;size:32"                                           json:"repository_selection"`
	Account             Account        `gorm:"foreignKey:AccountID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"             json:"-"`
}

// TableName returns the database table name for GitHubInstallation.
func (GitHubInstallation) TableName() string {
	return "github_installations"
}

// GitHubRepository is the latest known repository snapshot for an installation.
type GitHubRepository struct {
	ID             string         `gorm:"column:id;primaryKey;size:64"                                                       json:"id"`
	CreatedAt      time.Time      `gorm:"column:created_at"                                                                  json:"created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"                                                                  json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at;index"                                                            json:"deleted_at,omitempty"`
	AccountID      string         `gorm:"column:account_id;size:64;not null;index;uniqueIndex:idx_account_installation_repo" json:"account_id"`
	InstallationID int64          `gorm:"column:installation_id;not null;uniqueIndex:idx_account_installation_repo"          json:"installation_id"`
	GitHubRepoID   int64          `gorm:"column:github_repo_id;not null;uniqueIndex:idx_account_installation_repo"           json:"github_repo_id"`
	Owner          string         `gorm:"column:owner;size:255;not null;index"                                               json:"owner"`
	Name           string         `gorm:"column:name;size:255;not null;index"                                                json:"name"`
	FullName       string         `gorm:"column:full_name;size:511;not null;index"                                           json:"full_name"`
	Private        bool           `gorm:"column:private"                                                                     json:"private"`
	DefaultBranch  string         `gorm:"column:default_branch;size:255"                                                     json:"default_branch"`
	HTMLURL        string         `gorm:"column:html_url;size:1024"                                                          json:"html_url"`
	Account        Account        `gorm:"foreignKey:AccountID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"                  json:"-"`
}

// TableName returns the database table name for GitHubRepository.
func (GitHubRepository) TableName() string {
	return "github_repositories"
}

// Workspace records a configured repository workspace.
type Workspace struct {
	ID            string         `gorm:"column:id;primaryKey;size:64"                                                    json:"id"`
	CreatedAt     time.Time      `gorm:"column:created_at"                                                               json:"created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"                                                               json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at;index"                                                         json:"deleted_at,omitempty"`
	AccountID     string         `gorm:"column:account_id;size:64;not null;index;uniqueIndex:idx_account_workspace_repo" json:"account_id"`
	Name          string         `gorm:"column:name;size:255;index"                                                      json:"name,omitempty"`
	GitHubRepoID  *int64         `gorm:"column:github_repo_id;uniqueIndex:idx_account_workspace_repo"                    json:"github_repo_id,omitempty"`
	RepoFullName  string         `gorm:"column:repo_full_name;size:511;index"                                            json:"repo_full_name,omitempty"`
	Region        string         `gorm:"column:region;size:255;not null"                                                 json:"region"`
	SandboxID     string         `gorm:"column:sandbox_id;size:255"                                                      json:"sandbox_id,omitempty"`
	TemplateID    string         `gorm:"column:template_id;size:255;not null"                                            json:"template_id"`
	State         string         `gorm:"column:state;size:64"                                                            json:"state,omitempty"`
	Endpoint      string         `gorm:"column:endpoint;size:1024"                                                       json:"endpoint,omitempty"`
	WorkspacePath string         `gorm:"column:workspace_path;size:1024"                                                 json:"workspace_path,omitempty"`
	IDEURL        string         `gorm:"column:ide_url;size:2048"                                                        json:"ide_url,omitempty"`
	Account       Account        `gorm:"foreignKey:AccountID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"               json:"-"`
}

// TableName returns the database table name for Workspace.
func (Workspace) TableName() string {
	return "workspaces"
}

// WorkspaceChatMessage stores an AI Chat turn for a workspace.
type WorkspaceChatMessage struct {
	ID          string         `gorm:"column:id;primaryKey;size:64"                                                   json:"id"`
	CreatedAt   time.Time      `gorm:"column:created_at;index"                                                        json:"created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"                                                              json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index"                                                        json:"deleted_at,omitempty"`
	AccountID   string         `gorm:"column:account_id;size:64;not null;index"                                       json:"account_id"`
	WorkspaceID string         `gorm:"column:workspace_id;size:64;not null;index:idx_workspace_chat_messages_scope"   json:"workspace_id"`
	SandboxID   string         `gorm:"column:sandbox_id;size:255;index"                                               json:"sandbox_id,omitempty"`
	Role        string         `gorm:"column:role;size:32;not null"                                                   json:"role"`
	Content     string         `gorm:"column:content;type:text;not null"                                              json:"content"`
	Provider    string         `gorm:"column:provider;size:32"                                                        json:"provider,omitempty"`
	ExitCode    int            `gorm:"column:exit_code"                                                               json:"exit_code,omitempty"`
	Workspace   Workspace      `gorm:"foreignKey:WorkspaceID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"            json:"-"`
	Account     Account        `gorm:"foreignKey:AccountID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"              json:"-"`
}

// TableName returns the database table name for WorkspaceChatMessage.
func (WorkspaceChatMessage) TableName() string {
	return "workspace_chat_messages"
}

// QiniuCredential stores encrypted Qiniu Cloud credentials for an account.
type QiniuCredential struct {
	ID                  string         `gorm:"column:id;primaryKey;size:64"                                      json:"id"`
	CreatedAt           time.Time      `gorm:"column:created_at"                                                 json:"created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"                                                 json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"column:deleted_at;index"                                           json:"deleted_at,omitempty"`
	AccountID           string         `gorm:"column:account_id;size:64;not null;uniqueIndex"                    json:"account_id"`
	KeyHint             string         `gorm:"column:key_hint;size:32"                                           json:"key_hint"`
	EncryptedAPIKey     string         `gorm:"column:encrypted_api_key;type:text;not null"                       json:"-"`
	MAASKeyHint         string         `gorm:"column:maas_key_hint;size:32"                                      json:"maas_key_hint,omitempty"`
	EncryptedMAASAPIKey string         `gorm:"column:encrypted_maas_api_key;type:text"                           json:"-"`
	AccessKeyHint       string         `gorm:"column:access_key_hint;size:32"                                    json:"access_key_hint,omitempty"`
	EncryptedAccessKey  string         `gorm:"column:encrypted_access_key;type:text"                             json:"-"`
	SecretKeyHint       string         `gorm:"column:secret_key_hint;size:32"                                    json:"secret_key_hint,omitempty"`
	EncryptedSecretKey  string         `gorm:"column:encrypted_secret_key;type:text"                             json:"-"`
	Account             Account        `gorm:"foreignKey:AccountID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

// TableName returns the database table name for QiniuCredential.
func (QiniuCredential) TableName() string {
	return "qiniu_credentials"
}

// SandboxSession records a Qiniu Sandbox instance owned by an account.
type SandboxSession struct {
	ID              string          `gorm:"column:id;primaryKey;size:64"                                             json:"id"`
	CreatedAt       time.Time       `gorm:"column:created_at"                                                        json:"created_at"`
	UpdatedAt       time.Time       `gorm:"column:updated_at"                                                        json:"updated_at"`
	DeletedAt       gorm.DeletedAt  `gorm:"column:deleted_at;index"                                                  json:"deleted_at,omitempty"`
	AccountID       string          `gorm:"column:account_id;size:64;not null;index;uniqueIndex:idx_account_sandbox" json:"account_id"`
	SandboxID       string          `gorm:"column:sandbox_id;size:255;not null;uniqueIndex:idx_account_sandbox"      json:"sandbox_id"`
	TemplateID      string          `gorm:"column:template_id;size:255;not null"                                     json:"template_id"`
	State           string          `gorm:"column:state;size:64;not null"                                            json:"state"`
	Endpoint        string          `gorm:"column:endpoint;size:1024"                                                json:"endpoint"`
	GitHubRepoID    *int64          `gorm:"column:github_repo_id"                                                    json:"github_repo_id,omitempty"`
	RepoFullName    string          `gorm:"column:repo_full_name;size:511"                                           json:"repo_full_name,omitempty"`
	WorkspacePath   string          `gorm:"column:workspace_path;size:1024"                                          json:"workspace_path,omitempty"`
	Region          string          `gorm:"column:region;size:255"                                                   json:"region,omitempty"`
	CPUCount        int32           `gorm:"column:cpu_count"                                                         json:"cpu_count,omitempty"`
	MemoryGB        int32           `gorm:"column:memory_gb"                                                         json:"memory_gb,omitempty"`
	IDEURL          string          `gorm:"column:ide_url;size:2048"                                                 json:"ide_url,omitempty"`
	Metadata        SandboxMetadata `gorm:"column:metadata;type:text"                                             json:"metadata,omitempty"`
	LastConnectedAt *time.Time      `gorm:"column:last_connected_at"                                                 json:"last_connected_at,omitempty"`
	Account         Account         `gorm:"foreignKey:AccountID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"        json:"-"`
}

// TableName returns the database table name for SandboxSession.
func (SandboxSession) TableName() string {
	return "sandbox_sessions"
}
