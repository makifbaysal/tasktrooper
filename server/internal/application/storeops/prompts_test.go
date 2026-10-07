package storeops

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
)

// TestGolden_BlockedReleaseRemedy pins the exact byte output of the release-
// blocked remedy sentence — posted both as a task comment and baked into the
// blocked-release task's own Description, both read by whichever agent picks
// the task up next.
func TestGolden_BlockedReleaseRemedy(t *testing.T) {
	want := "Enable GitHub Actions for this repository (or settle its billing), or pair a machine as a local runner (iOS releases need a Mac), then start the release again."
	if got := blockedReleaseRemedy(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestGolden_BuildTargetRemedy pins engine.go's buildTargetRemedy wording —
// the build.Error a batch/store deploy leaves on ReleaseStoreBuild when the
// repository states no build target, read back by get_release/watch_release.
func TestGolden_BuildTargetRemedy(t *testing.T) {
	assert := func(got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	assert(buildTargetRemedy(pipeline.Spec{Platform: "ios"}),
		"no shared Xcode scheme was found in the working copy: share the app's scheme in Xcode (Product › Scheme › Manage Schemes, tick Shared), commit the .xcscheme file it writes under xcshareddata/xcschemes, and re-import the repository")
	assert(buildTargetRemedy(pipeline.Spec{Platform: "android"}),
		"no Gradle module applying com.android.application was found in the working copy: make sure settings.gradle includes the app module and that its build.gradle applies that plugin, then re-import the repository")
}
