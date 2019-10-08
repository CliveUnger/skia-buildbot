package fs_expstore

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	assert "github.com/stretchr/testify/require"
	"go.skia.org/infra/go/deepequal"
	"go.skia.org/infra/go/eventbus/mocks"
	"go.skia.org/infra/go/firestore"
	"go.skia.org/infra/go/testutils/unittest"
	"go.skia.org/infra/golden/go/expstorage"
	data "go.skia.org/infra/golden/go/testutils/data_three_devices"
	"go.skia.org/infra/golden/go/types"
	"go.skia.org/infra/golden/go/types/expectations"
)

// TestGetExpectations writes some changes and then reads back the
// aggregated results.
func TestGetExpectations(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	// Brand new instance should have no expectations
	e, err := f.Get()
	assert.NoError(t, err)
	assert.Equal(t, expectations.Expectations{}, e)

	err = f.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaUntriaged1Digest: expectations.Positive,
			data.AlphaGood1Digest:      expectations.Positive,
		},
	}, userOne)
	assert.NoError(t, err)

	err = f.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaBad1Digest:       expectations.Negative,
			data.AlphaUntriaged1Digest: expectations.Untriaged, // overwrites previous
		},
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}, userTwo)
	assert.NoError(t, err)

	expected := expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest:      expectations.Positive,
			data.AlphaBad1Digest:       expectations.Negative,
			data.AlphaUntriaged1Digest: expectations.Untriaged,
		},
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}

	e, err = f.Get()
	assert.NoError(t, err)
	assert.Equal(t, expected, e)

	// Make sure that if we create a new view, we can read the results immediately.
	fr, err := New(ctx, c, nil, ReadOnly)
	assert.NoError(t, err)
	e, err = fr.Get()
	assert.NoError(t, err)
	assert.Equal(t, expected, e)
}

// TestGetExpectationsSnapShot has both a read-write and a read version and makes sure
// that the changes to the read-write version eventually propagate to the read version
// via the QuerySnapshot.
func TestGetExpectationsSnapShot(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	err = f.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaUntriaged1Digest: expectations.Positive,
			data.AlphaGood1Digest:      expectations.Positive,
		},
	}, userOne)
	assert.NoError(t, err)

	ro, err := New(ctx, c, nil, ReadOnly)
	assert.NoError(t, err)
	assert.NotNil(t, ro)

	exp, err := ro.Get()
	assert.NoError(t, err)
	assert.Equal(t, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaUntriaged1Digest: expectations.Positive,
			data.AlphaGood1Digest:      expectations.Positive,
		},
	}, exp)

	err = f.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaBad1Digest:       expectations.Negative,
			data.AlphaUntriaged1Digest: expectations.Untriaged, // overwrites previous
		},
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}, userTwo)
	assert.NoError(t, err)

	expected := expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest:      expectations.Positive,
			data.AlphaBad1Digest:       expectations.Negative,
			data.AlphaUntriaged1Digest: expectations.Untriaged,
		},
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}

	e, err := ro.Get()
	assert.NoError(t, err)
	assert.Equal(t, expected, e)
}

// TestGetExpectationsRace writes a bunch of data from many go routines
// in an effort to catch any race conditions in the caching layer.
func TestGetExpectationsRace(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	type entry struct {
		Grouping types.TestName
		Digest   types.Digest
		Label    expectations.Label
	}

	entries := []entry{
		{
			Grouping: data.AlphaTest,
			Digest:   data.AlphaUntriaged1Digest,
			Label:    expectations.Untriaged,
		},
		{
			Grouping: data.AlphaTest,
			Digest:   data.AlphaBad1Digest,
			Label:    expectations.Negative,
		},
		{
			Grouping: data.AlphaTest,
			Digest:   data.AlphaGood1Digest,
			Label:    expectations.Positive,
		},
		{
			Grouping: data.BetaTest,
			Digest:   data.BetaGood1Digest,
			Label:    expectations.Positive,
		},
		{
			Grouping: data.BetaTest,
			Digest:   data.BetaUntriaged1Digest,
			Label:    expectations.Untriaged,
		},
	}

	wg := sync.WaitGroup{}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := entries[i%len(entries)]
			err := f.AddChange(ctx, expectations.Expectations{
				e.Grouping: {
					e.Digest: e.Label,
				},
			}, userOne)
			assert.NoError(t, err)
		}(i)

		// Make sure we can read and write w/o races
		if i%5 == 0 {
			_, err := f.Get()
			assert.NoError(t, err)
		}
	}

	wg.Wait()

	e, err := f.Get()
	assert.NoError(t, err)
	assert.Equal(t, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest:      expectations.Positive,
			data.AlphaBad1Digest:       expectations.Negative,
			data.AlphaUntriaged1Digest: expectations.Untriaged,
		},
		data.BetaTest: {
			data.BetaGood1Digest:      expectations.Positive,
			data.BetaUntriaged1Digest: expectations.Untriaged,
		},
	}, e)
}

// TestGetExpectationsBig writes 32^2=1024 entries
// to test the batch writing.
func TestGetExpectationsBig(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	// Write the expectations in two, non-overlapping blocks.
	exp1 := makeBigExpectations(0, 16)
	exp2 := makeBigExpectations(16, 32)

	expected := exp1.DeepCopy()
	expected.MergeExpectations(exp2)

	wg := sync.WaitGroup{}

	// Write them concurrently to test for races.
	wg.Add(2)
	go func() {
		defer wg.Done()
		err := f.AddChange(ctx, exp1, userOne)
		assert.NoError(t, err)
	}()
	go func() {
		defer wg.Done()
		err := f.AddChange(ctx, exp2, userTwo)
		assert.NoError(t, err)
	}()
	wg.Wait()

	// We wait for the query snapshots to be notified about the change.
	assert.Eventually(t, func() bool {
		e, err := f.Get()
		assert.NoError(t, err)
		return deepequal.DeepEqual(expected, e)
	}, 10*time.Second, 100*time.Millisecond)

	// Make sure that if we create a new view, we can read the results
	// from the table to make the expectations
	fr, err := New(ctx, c, nil, ReadOnly)
	assert.NoError(t, err)
	e, err := fr.Get()
	assert.NoError(t, err)
	assert.Equal(t, expected, e)
}

// TestReadOnly ensures a read-only instance fails to write data.
func TestReadOnly(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadOnly)
	assert.NoError(t, err)

	err = f.AddChange(context.Background(), expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Positive,
		},
	}, userOne)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")
}

// TestQueryLog tests that we can query logs at a given place
func TestQueryLog(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()
	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	fillWith4Entries(t, f)

	entries, n, err := f.QueryLog(ctx, 0, 100, false)
	assert.NoError(t, err)
	assert.Equal(t, 4, n) // 4 operations

	now := time.Now()
	nowMS := now.Unix() * 1000
	normalizeEntries(t, now, entries)
	assert.Equal(t, []expstorage.TriageLogEntry{
		{
			ID:          "was_random_0",
			Name:        userTwo,
			TS:          nowMS,
			ChangeCount: 2,
			Details:     nil,
		},
		{
			ID:          "was_random_1",
			Name:        userOne,
			TS:          nowMS,
			ChangeCount: 1,
			Details:     nil,
		},
		{
			ID:          "was_random_2",
			Name:        userTwo,
			TS:          nowMS,
			ChangeCount: 1,
			Details:     nil,
		},
		{
			ID:          "was_random_3",
			Name:        userOne,
			TS:          nowMS,
			ChangeCount: 1,
			Details:     nil,
		},
	}, entries)

	entries, n, err = f.QueryLog(ctx, 1, 2, false)
	assert.NoError(t, err)
	assert.Equal(t, 2, n)
	normalizeEntries(t, now, entries)
	assert.Equal(t, []expstorage.TriageLogEntry{
		{
			ID:          "was_random_0",
			Name:        userOne,
			TS:          nowMS,
			ChangeCount: 1,
			Details:     nil,
		},
		{
			ID:          "was_random_1",
			Name:        userTwo,
			TS:          nowMS,
			ChangeCount: 1,
			Details:     nil,
		},
	}, entries)

	// Make sure we can handle an invalid offset
	entries, n, err = f.QueryLog(ctx, 500, 100, false)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Nil(t, entries)
}

// TestQueryLogDetails checks that the details are filled in when requested.
func TestQueryLogDetails(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	fillWith4Entries(t, f)

	entries, n, err := f.QueryLog(ctx, 0, 100, true)
	assert.NoError(t, err)
	assert.Equal(t, 4, n) // 4 operations

	assert.Equal(t, []expstorage.TriageDetail{
		{
			TestName: data.AlphaTest,
			Digest:   data.AlphaBad1Digest,
			Label:    expectations.Negative.String(),
		},
		{
			TestName: data.BetaTest,
			Digest:   data.BetaUntriaged1Digest,
			Label:    expectations.Untriaged.String(),
		},
	}, entries[0].Details)
	assert.Equal(t, []expstorage.TriageDetail{
		{
			TestName: data.BetaTest,
			Digest:   data.BetaGood1Digest,
			Label:    expectations.Positive.String(),
		},
	}, entries[1].Details)
	assert.Equal(t, []expstorage.TriageDetail{
		{
			TestName: data.AlphaTest,
			Digest:   data.AlphaGood1Digest,
			Label:    expectations.Positive.String(),
		},
	}, entries[2].Details)
	assert.Equal(t, []expstorage.TriageDetail{
		{
			TestName: data.AlphaTest,
			Digest:   data.AlphaGood1Digest,
			Label:    expectations.Negative.String(),
		},
	}, entries[3].Details)
}

// TestQueryLogDetailsLarge checks that the details are filled in correctly, even in cases
// where we had to write in multiple chunks. (skbug.com/9485)
func TestQueryLogDetailsLarge(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	// 800 should spread us across 3 "shards", which are ~250 expectations.
	const numExp = 800
	delta := expectations.Expectations{}
	for i := uint64(0); i < numExp; i++ {
		n := types.TestName(fmt.Sprintf("test_%03d", i))
		// An MD5 hash is 128 bits, which is 32 chars
		d := types.Digest(fmt.Sprintf("%032d", i))
		delta.AddDigest(n, d, expectations.Positive)
	}
	err = f.AddChange(ctx, delta, "test@example.com")
	assert.NoError(t, err)

	entries, n, err := f.QueryLog(ctx, 0, 2, true)
	assert.NoError(t, err)
	assert.Equal(t, 1, n) // 1 big operation

	entry := entries[0]
	assert.Equal(t, numExp, entry.ChangeCount)
	assert.Len(t, entry.Details, numExp)

	// spot check some details
	assert.Equal(t, expstorage.TriageDetail{
		TestName: "test_000",
		Digest:   "00000000000000000000000000000000",
		Label:    expectations.Positive.String(),
	}, entry.Details[0])
	assert.Equal(t, expstorage.TriageDetail{
		TestName: "test_200",
		Digest:   "00000000000000000000000000000200",
		Label:    expectations.Positive.String(),
	}, entry.Details[200])
	assert.Equal(t, expstorage.TriageDetail{
		TestName: "test_400",
		Digest:   "00000000000000000000000000000400",
		Label:    expectations.Positive.String(),
	}, entry.Details[400])
	assert.Equal(t, expstorage.TriageDetail{
		TestName: "test_600",
		Digest:   "00000000000000000000000000000600",
		Label:    expectations.Positive.String(),
	}, entry.Details[600])
	assert.Equal(t, expstorage.TriageDetail{
		TestName: "test_799",
		Digest:   "00000000000000000000000000000799",
		Label:    expectations.Positive.String(),
	}, entry.Details[799])
}

// TestUndoChangeSunnyDay checks undoing entries that exist.
func TestUndoChangeSunnyDay(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	fillWith4Entries(t, f)

	entries, n, err := f.QueryLog(ctx, 0, 4, false)
	assert.NoError(t, err)
	assert.Equal(t, 4, n)

	exp, err := f.UndoChange(ctx, entries[0].ID, userOne)
	assert.NoError(t, err)
	assert.Equal(t, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaBad1Digest: expectations.Untriaged,
		},
		data.BetaTest: {
			data.BetaUntriaged1Digest: expectations.Untriaged,
		},
	}, exp)

	exp, err = f.UndoChange(ctx, entries[2].ID, userOne)
	assert.NoError(t, err)
	assert.Equal(t, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Negative,
		},
	}, exp)

	expected := expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Negative,
			data.AlphaBad1Digest:  expectations.Untriaged,
		},
		data.BetaTest: {
			data.BetaGood1Digest:      expectations.Positive,
			data.BetaUntriaged1Digest: expectations.Untriaged,
		},
	}

	// Check that the undone items were applied
	exp, err = f.Get()
	assert.NoError(t, err)
	assert.Equal(t, expected, exp)

	// Make sure that if we create a new view, we can read the results
	// from the table to make the expectations
	fr, err := New(ctx, c, nil, ReadOnly)
	assert.NoError(t, err)
	exp, err = fr.Get()
	assert.NoError(t, err)
	assert.Equal(t, expected, exp)
}

// TestUndoChangeNoExist checks undoing an entry that does not exist.
func TestUndoChangeNoExist(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	_, err = f.UndoChange(ctx, "doesnotexist", "userTwo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not find change")
}

// TestEventBusAddMaster makes sure proper eventbus signals are sent
// when changes are made to the master branch.
func TestEventBusAddMaster(t *testing.T) {
	unittest.LargeTest(t)

	meb := &mocks.EventBus{}
	defer meb.AssertExpectations(t)

	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, meb, ReadWrite)
	assert.NoError(t, err)

	change1 := expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Positive,
		},
	}
	change2 := expectations.Expectations{
		data.AlphaTest: {
			data.AlphaBad1Digest: expectations.Negative,
		},
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}

	meb.On("Publish", expstorage.EV_EXPSTORAGE_CHANGED, &expstorage.EventExpectationChange{
		ExpectationDelta: change1,
		CRSAndCLID:       "",
	}, /*global=*/ true).Once()
	// This was two entries, which are split up into two firestore records. Thus, we should
	// see two events, one for each of them.
	meb.On("Publish", expstorage.EV_EXPSTORAGE_CHANGED, &expstorage.EventExpectationChange{
		ExpectationDelta: expectations.Expectations{
			data.AlphaTest: {
				data.AlphaBad1Digest: expectations.Negative,
			},
		},
		CRSAndCLID: "",
	}, /*global=*/ true).Once()
	meb.On("Publish", expstorage.EV_EXPSTORAGE_CHANGED, &expstorage.EventExpectationChange{
		ExpectationDelta: expectations.Expectations{
			data.BetaTest: {
				data.BetaGood1Digest: expectations.Positive,
			},
		},
		CRSAndCLID: "",
	}, /*global=*/ true).Once()

	assert.NoError(t, f.AddChange(ctx, change1, userOne))
	assert.NoError(t, f.AddChange(ctx, change2, userTwo))
}

// TestEventBusUndo tests that eventbus signals are properly sent during Undo.
func TestEventBusUndo(t *testing.T) {
	unittest.LargeTest(t)

	meb := &mocks.EventBus{}
	defer meb.AssertExpectations(t)

	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	f, err := New(ctx, c, meb, ReadWrite)
	assert.NoError(t, err)

	change := expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Negative,
		},
	}
	expectedUndo := expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Untriaged,
		},
	}

	meb.On("Publish", expstorage.EV_EXPSTORAGE_CHANGED, &expstorage.EventExpectationChange{
		ExpectationDelta: change,
		CRSAndCLID:       "",
	}, /*global=*/ true).Once()
	meb.On("Publish", expstorage.EV_EXPSTORAGE_CHANGED, &expstorage.EventExpectationChange{
		ExpectationDelta: expectedUndo,
		CRSAndCLID:       "",
	}, /*global=*/ true).Once()

	assert.NoError(t, f.AddChange(ctx, change, userOne))

	entries, n, err := f.QueryLog(ctx, 0, 1, false)
	assert.NoError(t, err)
	assert.Equal(t, 1, n)

	exp, err := f.UndoChange(ctx, entries[0].ID, userOne)
	assert.NoError(t, err)
	assert.Equal(t, expectedUndo, exp)
}

// TestCLExpectationsAddGet tests the separation of the MasterExpectations
// and the CLExpectations. It starts with a shared history, then
// adds some expectations to both, before asserting that they are properly dealt
// with. Specifically, the CLExpectations should be treated as a delta to
// the MasterExpectations (but doesn't actually contain MasterExpectations).
func TestCLExpectationsAddGet(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	mb, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	assert.NoError(t, mb.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Negative,
		},
	}, userTwo))

	ib := mb.ForChangeList("117", "gerrit") // arbitrary cl id

	// Check that it starts out blank.
	clExp, err := ib.Get()
	assert.NoError(t, err)
	assert.Equal(t, expectations.Expectations{}, clExp)

	// Add to the CLExpectations
	assert.NoError(t, ib.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Positive,
		},
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}, userOne))

	// Add to the MasterExpectations
	assert.NoError(t, mb.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaBad1Digest: expectations.Negative,
		},
	}, userOne))

	masterE, err := mb.Get()
	assert.NoError(t, err)
	clExp, err = ib.Get()
	assert.NoError(t, err)

	// Make sure the CLExpectations did not leak to the MasterExpectations
	assert.Equal(t, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Negative,
			data.AlphaBad1Digest:  expectations.Negative,
		},
	}, masterE)

	// Make sure the CLExpectations are separate from the MasterExpectations.
	assert.Equal(t, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Positive,
		},
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}, clExp)
}

// TestCLExpectationsQueryLog makes sure the QueryLogs interacts
// with the CLExpectations as expected. Which is to say, the two
// logs are separate.
func TestCLExpectationsQueryLog(t *testing.T) {
	unittest.LargeTest(t)
	c, cleanup := firestore.NewClientForTesting(t)
	defer cleanup()
	ctx := context.Background()

	mb, err := New(ctx, c, nil, ReadWrite)
	assert.NoError(t, err)

	assert.NoError(t, mb.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Positive,
		},
	}, userTwo))

	ib := mb.ForChangeList("117", "gerrit") // arbitrary cl id

	assert.NoError(t, ib.AddChange(ctx, expectations.Expectations{
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}, userOne))

	// Make sure the master logs are separate from the cl logs.
	// request up to 10 to make sure we would get the cl
	// change (if the filtering was wrong).
	entries, n, err := mb.QueryLog(ctx, 0, 10, true)
	assert.NoError(t, err)
	assert.Equal(t, 1, n)

	now := time.Now()
	nowMS := now.Unix() * 1000
	normalizeEntries(t, now, entries)
	assert.Equal(t, expstorage.TriageLogEntry{
		ID:          "was_random_0",
		Name:        userTwo,
		TS:          nowMS,
		ChangeCount: 1,
		Details: []expstorage.TriageDetail{
			{
				TestName: data.AlphaTest,
				Digest:   data.AlphaGood1Digest,
				Label:    expectations.Positive.String(),
			},
		},
	}, entries[0])

	// Make sure the cl logs are separate from the master logs.
	// Unlike when getting the expectations, the cl logs are
	// *only* those logs that affected this cl. Not, for example,
	// all the master logs with the cl logs tacked on.
	entries, n, err = ib.QueryLog(ctx, 0, 10, true)
	assert.NoError(t, err)
	assert.Equal(t, 1, n) // only one change on this branch

	normalizeEntries(t, now, entries)
	assert.Equal(t, expstorage.TriageLogEntry{
		ID:          "was_random_0",
		Name:        userOne,
		TS:          nowMS,
		ChangeCount: 1,
		Details: []expstorage.TriageDetail{
			{
				TestName: data.BetaTest,
				Digest:   data.BetaGood1Digest,
				Label:    expectations.Positive.String(),
			},
		},
	}, entries[0])
}

// TestExpectationEntryID tests edge cases for malformed names
func TestExpectationEntryID(t *testing.T) {
	unittest.SmallTest(t)
	// Based on real data
	e := expectationEntry{
		Grouping: "downsample/images/mandrill_512.png",
		Digest:   "36bc7da524f2869c97f0a0f1d7042110",
	}
	assert.Equal(t, "downsample-images-mandrill_512.png|36bc7da524f2869c97f0a0f1d7042110",
		e.ID())
}

// fillWith4Entries fills a given Store with 4 triaged records of a few digests.
func fillWith4Entries(t *testing.T, f *Store) {
	ctx := context.Background()
	assert.NoError(t, f.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Negative,
		},
	}, userOne))
	assert.NoError(t, f.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaGood1Digest: expectations.Positive, // overwrites previous value
		},
	}, userTwo))
	assert.NoError(t, f.AddChange(ctx, expectations.Expectations{
		data.BetaTest: {
			data.BetaGood1Digest: expectations.Positive,
		},
	}, userOne))
	assert.NoError(t, f.AddChange(ctx, expectations.Expectations{
		data.AlphaTest: {
			data.AlphaBad1Digest: expectations.Negative,
		},
		data.BetaTest: {
			data.BetaUntriaged1Digest: expectations.Untriaged,
		},
	}, userTwo))
}

// Some parts of the entries (timestamp and id) are non-deterministic
// Make sure they are valid, then replace them with deterministic values
// for an easier comparison.
func normalizeEntries(t *testing.T, now time.Time, entries []expstorage.TriageLogEntry) {
	for i, te := range entries {
		assert.NotEqual(t, "", te.ID)
		te.ID = "was_random_" + strconv.Itoa(i)
		ts := time.Unix(te.TS/1000, 0)
		assert.False(t, ts.IsZero())
		assert.True(t, now.After(ts))
		te.TS = now.Unix() * 1000
		entries[i] = te
	}
}

// makeBigExpectations makes n tests named from start to end that each have 32 digests.
func makeBigExpectations(start, end int) expectations.Expectations {
	e := expectations.Expectations{}
	for i := start; i < end; i++ {
		for j := 0; j < 32; j++ {
			e.AddDigest(types.TestName(fmt.Sprintf("test-%03d", i)),
				types.Digest(fmt.Sprintf("digest-%03d", j)), expectations.Positive)
		}
	}
	return e
}

const (
	userOne = "userOne@example.com"
	userTwo = "userTwo@example.com"
)
