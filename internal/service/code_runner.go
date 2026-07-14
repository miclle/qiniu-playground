package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/miclle/qiniu-playground/internal/entity"
	"github.com/miclle/qiniu-playground/pkg/id"
	"gorm.io/gorm"
)

const (
	CodeRunnerLanguagePython     = "python"
	CodeRunnerLanguageJavaScript = "javascript"
	CodeRunnerLanguageTypeScript = "typescript"
	CodeRunnerLanguageR          = "r"
	CodeRunnerLanguageJava       = "java"
	CodeRunnerLanguageBash       = "bash"
)

// IsSupportedCodeRunnerLanguage reports whether a language is supported by the
// Code Runner surface. The list mirrors E2B Code Interpreter's built-in runtimes.
func IsSupportedCodeRunnerLanguage(language string) bool {
	switch language {
	case CodeRunnerLanguagePython,
		CodeRunnerLanguageJavaScript,
		CodeRunnerLanguageTypeScript,
		CodeRunnerLanguageR,
		CodeRunnerLanguageJava,
		CodeRunnerLanguageBash:
		return true
	default:
		return false
	}
}

// CodeRunnerSessionInput is the normalized code runner session payload.
type CodeRunnerSessionInput struct {
	ID            string
	Name          string
	Region        string
	SandboxID     string
	TemplateID    string
	State         string
	Endpoint      string
	WorkspacePath string
}

// CodeRunInput is the normalized code execution payload.
type CodeRunInput struct {
	SessionID  string
	SandboxID  string
	Language   string
	Code       string
	Stdin      string
	Stdout     string
	Stderr     string
	Error      string
	ExitCode   int
	DurationMS int64
	Metadata   map[string]string
}

// SaveCodeRunnerSession stores a code runner session.
func (s *Service) SaveCodeRunnerSession(ctx context.Context, accountID string, input CodeRunnerSessionInput) (*entity.CodeRunnerSession, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if strings.TrimSpace(input.Region) == "" {
		return nil, fmt.Errorf("region is required")
	}
	if strings.TrimSpace(input.TemplateID) == "" {
		return nil, fmt.Errorf("template id is required")
	}
	sessionID := input.ID
	if sessionID == "" {
		var err error
		sessionID, err = id.NewPrefixed("crs")
		if err != nil {
			return nil, err
		}
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Untitled"
	}
	session := entity.CodeRunnerSession{
		ID:            sessionID,
		AccountID:     accountID,
		Name:          name,
		Region:        input.Region,
		SandboxID:     input.SandboxID,
		TemplateID:    input.TemplateID,
		State:         input.State,
		Endpoint:      input.Endpoint,
		WorkspacePath: input.WorkspacePath,
	}
	if err := s.db.WithContext(ctx).Save(&session).Error; err != nil {
		return nil, fmt.Errorf("save code runner session: %w", err)
	}
	return &session, nil
}

// SaveCodeRunnerSessionWithSandbox atomically stores the Code Runner and sandbox views.
func (s *Service) SaveCodeRunnerSessionWithSandbox(
	ctx context.Context,
	accountID string,
	sessionInput CodeRunnerSessionInput,
	sandboxInput SandboxSessionInput,
) (*entity.CodeRunnerSession, error) {
	var session *entity.CodeRunnerSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txService := &Service{db: tx}
		var err error
		session, err = txService.SaveCodeRunnerSession(ctx, accountID, sessionInput)
		if err != nil {
			return err
		}
		metadata := make(map[string]string, len(sandboxInput.Metadata)+1)
		for key, value := range sandboxInput.Metadata {
			metadata[key] = value
		}
		metadata["session_id"] = session.ID
		sandboxInput.Metadata = metadata
		_, err = txService.SaveSandboxSession(ctx, accountID, sandboxInput)
		return err
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

// CodeRunnerSession returns a code runner session owned by an account.
func (s *Service) CodeRunnerSession(ctx context.Context, accountID, sessionID string) (*entity.CodeRunnerSession, error) {
	var session entity.CodeRunnerSession
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND id = ?", accountID, sessionID).
		First(&session).Error
	if err != nil {
		return nil, fmt.Errorf("find code runner session: %w", err)
	}
	return &session, nil
}

// ListCodeRunnerSessions returns code runner sessions for an account.
func (s *Service) ListCodeRunnerSessions(ctx context.Context, accountID string) ([]entity.CodeRunnerSession, error) {
	var sessions []entity.CodeRunnerSession
	err := s.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("updated_at desc").
		Find(&sessions).Error
	if err != nil {
		return nil, fmt.Errorf("list code runner sessions: %w", err)
	}
	return sessions, nil
}

// SaveCodeRun stores one code execution result.
func (s *Service) SaveCodeRun(ctx context.Context, accountID string, input CodeRunInput) (*entity.CodeRun, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if !IsSupportedCodeRunnerLanguage(input.Language) {
		return nil, fmt.Errorf("unsupported code language: %s", input.Language)
	}
	if strings.TrimSpace(input.Code) == "" {
		return nil, fmt.Errorf("code is required")
	}

	var latest entity.CodeRun
	err := s.db.WithContext(ctx).
		Where("account_id = ? AND session_id = ?", accountID, input.SessionID).
		Order("created_at DESC, id DESC").
		First(&latest).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("find latest code run: %w", err)
	}
	if err == nil && latest.Language == input.Language && latest.Code == input.Code && latest.Stdin == input.Stdin {
		now := time.Now()
		updates := map[string]any{
			"sandbox_id":  input.SandboxID,
			"stdout":      input.Stdout,
			"stderr":      input.Stderr,
			"error":       input.Error,
			"exit_code":   input.ExitCode,
			"duration_ms": input.DurationMS,
			"metadata":    entity.SandboxMetadata(input.Metadata),
			"created_at":  now,
			"updated_at":  now,
		}
		if err := s.db.WithContext(ctx).Model(&latest).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("update code run: %w", err)
		}
		latest.SandboxID = input.SandboxID
		latest.Stdout = input.Stdout
		latest.Stderr = input.Stderr
		latest.Error = input.Error
		latest.ExitCode = input.ExitCode
		latest.DurationMS = input.DurationMS
		latest.Metadata = entity.SandboxMetadata(input.Metadata)
		latest.CreatedAt = now
		latest.UpdatedAt = now
		return &latest, nil
	}

	runID, err := id.NewPrefixed("run")
	if err != nil {
		return nil, err
	}
	run := entity.CodeRun{
		ID:         runID,
		AccountID:  accountID,
		SessionID:  input.SessionID,
		SandboxID:  input.SandboxID,
		Language:   input.Language,
		Code:       input.Code,
		Stdin:      input.Stdin,
		Stdout:     input.Stdout,
		Stderr:     input.Stderr,
		Error:      input.Error,
		ExitCode:   input.ExitCode,
		DurationMS: input.DurationMS,
		Metadata:   entity.SandboxMetadata(input.Metadata),
	}
	if err := s.db.WithContext(ctx).Create(&run).Error; err != nil {
		return nil, fmt.Errorf("save code run: %w", err)
	}
	return &run, nil
}

// ListCodeRuns returns recent code execution history for a session.
func (s *Service) ListCodeRuns(ctx context.Context, accountID, sessionID string, limit int) ([]entity.CodeRun, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var runs []entity.CodeRun
	if err := s.db.WithContext(ctx).
		Where("account_id = ? AND session_id = ?", accountID, sessionID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list code runs: %w", err)
	}
	slices.Reverse(runs)
	return runs, nil
}
