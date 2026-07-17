package skill

import "testing"

func TestRegistryPluginSkillsOverrideGlobal(t *testing.T) {
	r := NewRegistry()
	r.Register(Skill{Name: "review", Description: "global"})
	r.SetPluginSkills("cad", []Skill{{Name: "review", Description: "cad"}, {Name: "draw"}})
	r.ActivatePlugin("cad")

	got, ok := r.Get("review")
	if !ok || got.Description != "cad" || r.Count() != 2 {
		t.Fatalf("unexpected active registry: got=%#v ok=%v count=%d", got, ok, r.Count())
	}
	r.DeactivatePlugin()
	got, ok = r.Get("review")
	if !ok || got.Description != "global" || r.Count() != 1 {
		t.Fatalf("unexpected global registry: got=%#v ok=%v count=%d", got, ok, r.Count())
	}
}

func TestRegistryReloadPreservesManualSkills(t *testing.T) {
	r := NewRegistry()
	r.Register(Skill{Name: "manual"})
	if err := r.AddLoader(NewLoader(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("manual"); !ok {
		t.Fatal("manual skill was lost during reload")
	}
}

func TestRegistryLoadedAndPluginLifecycle(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root+"/loaded")
	mustWrite(t, root+"/loaded/"+instructionFile, "# Loaded\n")
	r := NewRegistry()
	if err := r.AddLoader(NewLoader(root)); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("loaded"); !ok {
		t.Fatal("loaded skill missing")
	}
	r.SetPluginSkills("cad", []Skill{{Name: "draw"}})
	r.ActivatePlugin("cad")
	if _, ok := r.Get("draw"); !ok {
		t.Fatal("plugin skill missing")
	}
	r.ClearPluginSkills("cad")
	if _, ok := r.Get("draw"); ok {
		t.Fatal("plugin skill was not cleared")
	}
	r.Remove("loaded")
	if _, ok := r.Get("loaded"); ok {
		t.Fatal("loaded skill was not removed")
	}
}
