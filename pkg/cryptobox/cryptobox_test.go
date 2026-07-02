package cryptobox

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	box, err := New("test-encryption-key")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	encrypted, err := box.Encrypt("secret-value")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	if encrypted == "secret-value" {
		t.Fatal("encrypted value should not equal plaintext")
	}
	decrypted, err := box.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if decrypted != "secret-value" {
		t.Fatalf("decrypted = %q, want secret-value", decrypted)
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	box, err := New("test-encryption-key")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	encrypted, err := box.Encrypt("secret-value")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	other, err := New("other-key")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := other.Decrypt(encrypted); err == nil {
		t.Fatal("Decrypt should reject ciphertext encrypted with another key")
	}
}
