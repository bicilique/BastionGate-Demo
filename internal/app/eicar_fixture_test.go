package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestRepositoryEICARFixtureIsExact(t *testing.T) {
	content, err := os.ReadFile("../../testdata/avatar.php.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 68 {
		t.Fatalf("fixture length = %d, want 68", len(content))
	}
	digest := sha256.Sum256(content)
	if got := hex.EncodeToString(digest[:]); got != "275a021bbfb6489e54d471899f7db9d1663fc695ec2fe2a2c4538aabf651fd0f" {
		t.Fatalf("fixture SHA-256 = %s", got)
	}
}
