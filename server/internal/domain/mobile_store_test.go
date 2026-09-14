package domain

import "testing"

func TestMobileStoreStateTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{MobileStoreStateUnregistered, MobileStoreStateOnboarding, true},
		{MobileStoreStateOnboarding, MobileStoreStateTestReady, true},
		{MobileStoreStateTestReady, MobileStoreStateLive, true},
		{MobileStoreStateOnboarding, MobileStoreStateLive, false},
		{MobileStoreStateLive, MobileStoreStateTestReady, false},
		{MobileStoreStateUnregistered, MobileStoreStateLive, false},
		// re-verification may bounce test_ready back to onboarding (e.g. app deleted in console)
		{MobileStoreStateTestReady, MobileStoreStateOnboarding, true},
	}
	for _, c := range cases {
		app := MobileStoreApp{State: c.from}
		if got := app.CanTransition(c.to); got != c.ok {
			t.Errorf("CanTransition(%s -> %s) = %v, want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestIsStoreProvider(t *testing.T) {
	if !IsStoreProvider(DeployProviderAppStore) || !IsStoreProvider(DeployProviderGooglePlay) {
		t.Fatal("store providers must be recognized")
	}
	if IsStoreProvider(DeployProviderFly) {
		t.Fatal("fly is not a store provider")
	}
}

func TestValidDeployProviderIncludesStores(t *testing.T) {
	if !ValidDeployProvider(DeployProviderAppStore) || !ValidDeployProvider(DeployProviderGooglePlay) {
		t.Fatal("store providers must be valid deploy providers")
	}
}

func TestChecklistDone(t *testing.T) {
	app := MobileStoreApp{Checklist: []ChecklistItem{{Key: "a", Done: true}, {Key: "b", Done: false}}}
	if app.ChecklistDone() {
		t.Fatal("unfinished checklist reported done")
	}
	app.Checklist[1].Done = true
	if !app.ChecklistDone() {
		t.Fatal("finished checklist reported not done")
	}
}
