package ids

import "testing"

func TestNewUUIDv7(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewUUIDv7()
		if !IsUUIDv7(id) {
			t.Fatalf("generated invalid UUIDv7: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate UUIDv7: %s", id)
		}
		seen[id] = true
	}
}

func TestIsUUIDv7RejectsOtherVersions(t *testing.T) {
	cases := []string{
		"",
		"019a2f4e-7c1a-4b3d-9e21-4f6a8c2d1b04",
		"019a2f4e-7c1a-7b3d-0e21-4f6a8c2d1b04",
		"019A2F4E-7C1A-7B3D-9E21-4F6A8C2D1B04",
		"not-a-uuid",
	}
	for _, c := range cases {
		if IsUUIDv7(c) {
			t.Errorf("IsUUIDv7(%q) = true, want false", c)
		}
	}
}
