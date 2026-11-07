package db

import "testing"

func TestIsMatrixRoomIDAcceptsBothRoomVersionForms(t *testing.T) {
	cases := map[string]bool{
		"!abc:example.org": true,
		// Room version 12 ids carry no server part.
		"!YUr9Q5mLBXEntXflii1hXA-nnrLWMMXh6iiO3um6PBw": true,
		"":                  false,
		"!":                 false,
		"abc:example.org":   false,
		"@user:example.org": false,
		"!has space":        false,
	}
	for roomID, want := range cases {
		if got := IsMatrixRoomID(roomID); got != want {
			t.Errorf("IsMatrixRoomID(%q) = %v, want %v", roomID, got, want)
		}
	}
}
