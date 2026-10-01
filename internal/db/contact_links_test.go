package db

import (
	"path/filepath"
	"testing"
)

func TestAClaimHoldsUntilTheIdentityIsReviewed(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ClaimContactsForReview(database, []string{"@a:x", "@b:x"}, ContactLinkSourceAgent); err != nil {
		t.Fatal(err)
	}
	claimed, err := ActiveContactReviewClaims(database)
	if err != nil || !claimed["@a:x"] || !claimed["@b:x"] {
		t.Fatalf("both claimed: %v %v", claimed, err)
	}
	if err := MarkContactsReviewed(database, []string{"@a:x"}, ContactLinkSourceAgent); err != nil {
		t.Fatal(err)
	}
	claimed, _ = ActiveContactReviewClaims(database)
	if claimed["@a:x"] || !claimed["@b:x"] {
		t.Fatalf("a reviewed identity is no longer claimed: %v", claimed)
	}
	if _, err := database.Exec(`UPDATE contact_review_claims SET claimed_at = claimed_at - ?`, ContactReviewClaimLifetime+1); err != nil {
		t.Fatal(err)
	}
	if claimed, _ = ActiveContactReviewClaims(database); len(claimed) != 0 {
		t.Fatalf("an old claim lets go: %v", claimed)
	}
}
