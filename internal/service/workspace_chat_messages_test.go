package service

import (
	"context"
	"testing"
)

func TestWorkspaceChatMessagesPersistInOrder(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	first, err := svc.SaveWorkspaceChatMessage(ctx, "acct_1", WorkspaceChatMessageInput{
		WorkspaceID: "wks_1",
		SandboxID:   "sbox_1",
		Role:        WorkspaceChatRoleUser,
		Content:     "  inspect files  ",
	})
	if err != nil {
		t.Fatalf("save user message: %v", err)
	}
	second, err := svc.SaveWorkspaceChatMessage(ctx, "acct_1", WorkspaceChatMessageInput{
		WorkspaceID: "wks_1",
		SandboxID:   "sbox_1",
		Role:        WorkspaceChatRoleAssistant,
		Content:     "Found README.md",
		Provider:    "codex",
	})
	if err != nil {
		t.Fatalf("save assistant message: %v", err)
	}

	messages, err := svc.ListWorkspaceChatMessages(ctx, "acct_1", "wks_1", 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].ID != first.ID || messages[0].Content != "inspect files" {
		t.Fatalf("first message = %+v, want trimmed user message", messages[0])
	}
	if messages[1].ID != second.ID || messages[1].Provider != "codex" {
		t.Fatalf("second message = %+v, want assistant message", messages[1])
	}

	otherMessages, err := svc.ListWorkspaceChatMessages(ctx, "acct_2", "wks_1", 10)
	if err != nil {
		t.Fatalf("list other account messages: %v", err)
	}
	if len(otherMessages) != 0 {
		t.Fatalf("other account messages = %d, want 0", len(otherMessages))
	}
}

func TestWorkspaceChatMessagesLimitReturnsRecentMessagesInOrder(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	for index, content := range []string{"oldest", "middle", "newest"} {
		if _, err := svc.SaveWorkspaceChatMessage(ctx, "acct_1", WorkspaceChatMessageInput{
			WorkspaceID: "wks_1",
			SandboxID:   "sbox_1",
			Role:        WorkspaceChatRoleUser,
			Content:     content,
		}); err != nil {
			t.Fatalf("save message %d: %v", index, err)
		}
	}

	messages, err := svc.ListWorkspaceChatMessages(ctx, "acct_1", "wks_1", 2)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Content != "middle" || messages[1].Content != "newest" {
		t.Fatalf("messages = %+v, want most recent messages in chronological order", messages)
	}
}
