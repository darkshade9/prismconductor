package store

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func TestProviderEntityCRUD(t *testing.T) {
	s := openTempStore(t)

	// Empty list on fresh DB.
	pvs, err := s.ListProviderEntities()
	if err != nil {
		t.Fatalf("ListProviderEntities empty: %v", err)
	}
	if len(pvs) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(pvs))
	}

	p := types.ProviderEntity{
		ID:        "prov-1",
		Name:      "openai-default",
		Kind:      types.ProviderOpenAI,
		Endpoint:  "https://api.openai.com/v1",
		APIKey:    "sk-test",
		CreatedAt: time.Unix(1700000000, 0),
	}
	if err := s.SaveProviderEntity(p); err != nil {
		t.Fatalf("SaveProviderEntity: %v", err)
	}

	got, err := s.GetProviderEntity(p.ID)
	if err != nil {
		t.Fatalf("GetProviderEntity: %v", err)
	}
	if got.Name != p.Name || got.Kind != p.Kind || got.Endpoint != p.Endpoint || got.APIKey != p.APIKey {
		t.Fatalf("GetProviderEntity round-trip mismatch: %+v", got)
	}
	if !got.CreatedAt.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, time.Unix(1700000000, 0))
	}

	// Upsert: change name.
	p.Name = "openai-renamed"
	if err := s.SaveProviderEntity(p); err != nil {
		t.Fatalf("SaveProviderEntity upsert: %v", err)
	}
	got2, _ := s.GetProviderEntity(p.ID)
	if got2.Name != "openai-renamed" {
		t.Errorf("after upsert Name = %q, want %q", got2.Name, "openai-renamed")
	}

	// Delete succeeds when not referenced.
	if err := s.DeleteProviderEntity(p.ID); err != nil {
		t.Fatalf("DeleteProviderEntity: %v", err)
	}
	pvs2, _ := s.ListProviderEntities()
	if len(pvs2) != 0 {
		t.Errorf("expected empty after delete, got %d", len(pvs2))
	}
}

func TestDeleteProviderEntityBlocked(t *testing.T) {
	s := openTempStore(t)

	prov := types.ProviderEntity{
		ID:        "prov-block",
		Name:      "claude-default",
		Kind:      types.ProviderClaude,
		CreatedAt: time.Now(),
	}
	if err := s.SaveProviderEntity(prov); err != nil {
		t.Fatalf("SaveProviderEntity: %v", err)
	}

	pool := types.Pool{
		ID:         "pool-1",
		Name:       "test-pool",
		Provider:   types.ProviderClaude,
		Model:      "claude-opus-4-7",
		Capacity:   1,
		Enabled:    true,
		CreatedAt:  time.Now(),
		ProviderID: prov.ID,
	}
	if err := s.SavePool(pool); err != nil {
		t.Fatalf("SavePool: %v", err)
	}

	err := s.DeleteProviderEntity(prov.ID)
	if err == nil {
		t.Fatal("expected error when deleting referenced provider, got nil")
	}
	ref, ok := err.(ErrProviderReferenced)
	if !ok {
		t.Fatalf("expected ErrProviderReferenced, got %T: %v", err, err)
	}
	if len(ref.PoolIDs) != 1 || ref.PoolIDs[0] != pool.ID {
		t.Errorf("ErrProviderReferenced.PoolIDs = %v, want [%s]", ref.PoolIDs, pool.ID)
	}
}

func TestMigratePoolsToProviders(t *testing.T) {
	s := openTempStore(t)

	// Insert two pools with identical credentials and one with different creds.
	pools := []types.Pool{
		{ID: "p1", Name: "a", Provider: types.ProviderOpenAI, Endpoint: "http://ep1", APIKey: "k1", Model: "m", Capacity: 1, CreatedAt: time.Now()},
		{ID: "p2", Name: "b", Provider: types.ProviderOpenAI, Endpoint: "http://ep1", APIKey: "k1", Model: "m", Capacity: 1, CreatedAt: time.Now()},
		{ID: "p3", Name: "c", Provider: types.ProviderOllama, Endpoint: "http://ep2", APIKey: "", Model: "m", Capacity: 1, CreatedAt: time.Now()},
	}
	for _, p := range pools {
		if err := s.SavePool(p); err != nil {
			t.Fatalf("SavePool %s: %v", p.ID, err)
		}
	}

	created, updated, err := s.MigratePoolsToProviders()
	if err != nil {
		t.Fatalf("MigratePoolsToProviders: %v", err)
	}
	// 2 unique groups → 2 providers, 3 pools updated.
	if created != 2 {
		t.Errorf("created = %d, want 2", created)
	}
	if updated != 3 {
		t.Errorf("updated = %d, want 3", updated)
	}

	provs, _ := s.ListProviderEntities()
	if len(provs) != 2 {
		t.Errorf("ListProviderEntities = %d, want 2", len(provs))
	}

	// Idempotent: running again must be a no-op.
	created2, updated2, err2 := s.MigratePoolsToProviders()
	if err2 != nil {
		t.Fatalf("second run: %v", err2)
	}
	if created2 != 0 || updated2 != 0 {
		t.Errorf("second run not idempotent: created=%d updated=%d", created2, updated2)
	}

	// p1 and p2 must share the same provider_id.
	pp1, _ := s.GetPool("p1")
	pp2, _ := s.GetPool("p2")
	pp3, _ := s.GetPool("p3")
	if pp1.ProviderID == "" || pp2.ProviderID == "" || pp3.ProviderID == "" {
		t.Errorf("expected all pools to have provider_id set: p1=%q p2=%q p3=%q", pp1.ProviderID, pp2.ProviderID, pp3.ProviderID)
	}
	if pp1.ProviderID != pp2.ProviderID {
		t.Errorf("p1 and p2 should share provider_id: p1=%q p2=%q", pp1.ProviderID, pp2.ProviderID)
	}
	if pp1.ProviderID == pp3.ProviderID {
		t.Errorf("p1 and p3 should have different provider_id: both=%q", pp1.ProviderID)
	}
}
