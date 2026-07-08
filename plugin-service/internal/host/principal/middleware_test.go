package principal

import "testing"

func TestDefaultResolveMainSiteBaseCandidatesUsesDockerServiceNameOnly(t *testing.T) {
	got := defaultResolveMainSiteBaseCandidates(nil)
	want := []string{"http://sub2api:8080"}

	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d; values=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
