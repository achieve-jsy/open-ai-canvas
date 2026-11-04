package app

import (
	"errors"
	"testing"
)

func TestAdvertisingProductionCAS(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	draft := advertisingTestDraft()
	ad, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Production", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := svc.ConfirmAdvertisingProject("owner", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.CreateAdvertisingProduction("owner", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.CreateAdvertisingProduction("owner", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil || again.Production.ID != p.Production.ID {
		t.Fatalf("idempotent create: %+v %v", again, err)
	}
	if p.Production.Revision != 1 || p.Settings.MusicVolume == nil || *p.Settings.MusicVolume != 0.15 {
		t.Fatalf("defaults: %+v", p)
	}
	if _, err := svc.GetAdvertisingProduction("other", ad.Project.ID, p.Production.ID); err == nil {
		t.Fatal("cross user read")
	}
	zero := 0.0
	p.Settings.MusicVolume = &zero
	saved, err := svc.SaveAdvertisingProductionSettings("owner", ad.Project.ID, p.Production.ID, 1, p.Settings)
	if err != nil || saved.Production.Revision != 2 || *saved.Settings.MusicVolume != 0 {
		t.Fatalf("CAS: %+v %v", saved, err)
	}
	_, err = svc.SaveAdvertisingProductionSettings("owner", ad.Project.ID, p.Production.ID, 1, p.Settings)
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Fatalf("stale CAS: %v", err)
	}
	p.Settings.Selections = map[string]AdvertisingSelection{"not-a-shot": {}}
	if _, err := svc.SaveAdvertisingProductionSettings("owner", ad.Project.ID, p.Production.ID, 2, p.Settings); err == nil {
		t.Fatal("unknown shot accepted")
	}
	p.Settings.Selections = map[string]AdvertisingSelection{draft.Shots[0].ID: {VideoSubmissionID: "unowned"}}
	if _, err := svc.SaveAdvertisingProductionSettings("owner", ad.Project.ID, p.Production.ID, 2, p.Settings); err == nil {
		t.Fatal("unavailable candidate accepted")
	}
	old, err := svc.GetAdvertisingRevision("owner", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil || old.SourceRevision != 1 {
		t.Fatalf("confirmed revision mutated: %+v %v", old, err)
	}
}
