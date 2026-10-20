package service

import (
	"context"
	"errors"
	"testing"
)

type fakePublicIDStore struct {
	ids     map[int64]int64
	queries int
	fail    bool
}

func (f *fakePublicIDStore) GetUserPublicIDs(_ context.Context, userIDs []int64) (map[int64]int64, error) {
	f.queries++
	if f.fail {
		return nil, errors.New("db down")
	}
	out := map[int64]int64{}
	for _, id := range userIDs {
		if v, ok := f.ids[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func (f *fakePublicIDStore) FindUserIDByPublicID(_ context.Context, appID, publicID int64) (int64, error) {
	for id, v := range f.ids {
		if v == publicID && appID == 1 {
			return id, nil
		}
	}
	return 0, nil
}

func TestUserPublicIDsMapCachesAndSkipsMissing(t *testing.T) {
	store := &fakePublicIDStore{ids: map[int64]int64{7: 4829173056482917, 9: 1000000000000001}}
	p := NewUserPublicIDs(store)
	got := p.Map(context.Background(), []int64{7, 9, 9, 404, 0, -1})
	if got[7] != 4829173056482917 || got[9] != 1000000000000001 {
		t.Fatalf("unexpected mapping: %v", got)
	}
	if _, ok := got[404]; ok {
		t.Fatalf("deleted user should be absent: %v", got)
	}
	if p.Of(context.Background(), 7) != 4829173056482917 || store.queries != 1 {
		t.Fatalf("cached lookup should not query again, queries=%d", store.queries)
	}
	store.fail = true
	if p.Of(context.Background(), 404) != 0 {
		t.Fatal("failed lookup must yield 0")
	}
	if p.Of(context.Background(), 9) != 1000000000000001 {
		t.Fatal("cached value must survive store failures")
	}
}

func TestUserPublicIDsResolveIsAppScoped(t *testing.T) {
	p := NewUserPublicIDs(&fakePublicIDStore{ids: map[int64]int64{7: 4829173056482917}})
	if id, _ := p.Resolve(context.Background(), 1, 4829173056482917); id != 7 {
		t.Fatalf("resolve = %d", id)
	}
	if id, _ := p.Resolve(context.Background(), 2, 4829173056482917); id != 0 {
		t.Fatalf("other app must not resolve, got %d", id)
	}
	var nilResolver *UserPublicIDs
	if nilResolver.Of(context.Background(), 7) != 0 {
		t.Fatal("nil resolver must yield 0")
	}
}
