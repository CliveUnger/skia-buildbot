// Package indexer continuously creates an index of the test results
// as the tiles, expectations, and ignores change.
package indexer

import (
	"context"
	"net/url"
	"sync"
	"time"

	"go.skia.org/infra/go/eventbus"
	"go.skia.org/infra/go/metrics2"
	"go.skia.org/infra/go/paramtools"
	"go.skia.org/infra/go/skerr"
	"go.skia.org/infra/go/sklog"
	"go.skia.org/infra/go/tiling"
	"go.skia.org/infra/golden/go/blame"
	"go.skia.org/infra/golden/go/diff"
	"go.skia.org/infra/golden/go/digest_counter"
	"go.skia.org/infra/golden/go/digesttools"
	"go.skia.org/infra/golden/go/expstorage"
	"go.skia.org/infra/golden/go/paramsets"
	"go.skia.org/infra/golden/go/pdag"
	"go.skia.org/infra/golden/go/shared"
	"go.skia.org/infra/golden/go/storage"
	"go.skia.org/infra/golden/go/summary"
	"go.skia.org/infra/golden/go/tilesource"
	"go.skia.org/infra/golden/go/types"
	"go.skia.org/infra/golden/go/warmer"
)

const (
	// Event emitted when the indexer updates the search index.
	// Callback argument: *SearchIndex
	// TODO(kjlubick) is this used anymore?
	indexUpdatedEvent = "indexer:index-updated"

	// Metric to track the number of digests that do not have be uploaded by bots.
	knownHashesMetric = "known_digests"
)

// SearchIndex contains everything that is necessary to search
// our current knowledge about test results. It should be
// considered as immutable. Whenever the underlying data changes,
// a new index is calculated via a pdag.
type SearchIndex struct {
	searchIndexConfig
	// The indices of these arrays are the int values of types.IgnoreState
	dCounters         [2]digest_counter.DigestCounter
	summaries         [2]countsAndBlames
	paramsetSummaries [2]paramsets.ParamSummary

	cpxTile types.ComplexTile
	blamer  blame.Blamer

	// This is set by the indexing pipeline when we just want to update
	// individual tests that have changed.
	testNames types.TestNameSet
}

// countsAndBlame makes the type declaration of SearchIndex a little nicer to read.
type countsAndBlames []*summary.TriageStatus

type searchIndexConfig struct {
	diffStore         diff.DiffStore
	expectationsStore expstorage.ExpectationsStore
	gcsClient         storage.GCSClient
	warmer            warmer.DiffWarmer
}

// newSearchIndex creates a new instance of SearchIndex. It is not intended to
// be used outside of this package. SearchIndex instances are created by the
// Indexer and retrieved via GetIndex().
func newSearchIndex(sic searchIndexConfig, cpxTile types.ComplexTile) *SearchIndex {
	return &SearchIndex{
		searchIndexConfig: sic,
		// The indices of these slices are the int values of types.IgnoreState
		dCounters:         [2]digest_counter.DigestCounter{},
		summaries:         [2]countsAndBlames{},
		paramsetSummaries: [2]paramsets.ParamSummary{},
		cpxTile:           cpxTile,
	}
}

// SearchIndexForTesting returns filled in search index to be used when testing. Note that the
// indices of the arrays are the int values of types.IgnoreState
func SearchIndexForTesting(cpxTile types.ComplexTile, dc [2]digest_counter.DigestCounter, pm [2]paramsets.ParamSummary, exp expstorage.ExpectationsStore, b blame.Blamer) *SearchIndex {
	return &SearchIndex{
		searchIndexConfig: searchIndexConfig{
			expectationsStore: exp,
		},
		dCounters:         dc,
		summaries:         [2]countsAndBlames{},
		paramsetSummaries: pm,
		blamer:            b,
		cpxTile:           cpxTile,
	}
}

// Tile implements the IndexSearcher interface.
func (idx *SearchIndex) Tile() types.ComplexTile {
	return idx.cpxTile
}

// GetIgnoreMatcher implements the IndexSearcher interface.
func (idx *SearchIndex) GetIgnoreMatcher() paramtools.ParamMatcher {
	return idx.cpxTile.IgnoreRules()
}

// DigestCountsByTest implements the IndexSearcher interface.
func (idx *SearchIndex) DigestCountsByTest(is types.IgnoreState) map[types.TestName]digest_counter.DigestCount {
	return idx.dCounters[is].ByTest()
}

// MaxDigestsByTest implements the IndexSearcher interface.
func (idx *SearchIndex) MaxDigestsByTest(is types.IgnoreState) map[types.TestName]types.DigestSet {
	return idx.dCounters[is].MaxDigestsByTest()
}

// DigestCountsByTrace implements the IndexSearcher interface.
func (idx *SearchIndex) DigestCountsByTrace(is types.IgnoreState) map[tiling.TraceID]digest_counter.DigestCount {
	return idx.dCounters[is].ByTrace()
}

// DigestCountsByQuery implements the IndexSearcher interface.
func (idx *SearchIndex) DigestCountsByQuery(query url.Values, is types.IgnoreState) digest_counter.DigestCount {
	return idx.dCounters[is].ByQuery(idx.cpxTile.GetTile(is), query)
}

// GetSummaries implements the IndexSearcher interface.
func (idx *SearchIndex) GetSummaries(is types.IgnoreState) []*summary.TriageStatus {
	return idx.summaries[is]
}

// CalcSummaries implements the IndexSearcher interface.
func (idx *SearchIndex) CalcSummaries(query url.Values, is types.IgnoreState, head bool) ([]*summary.TriageStatus, error) {
	exp, err := idx.expectationsStore.Get()
	if err != nil {
		return nil, skerr.Wrap(err)
	}
	d := summary.Data{
		Traces:       idx.cpxTile.GetTile(is).Traces,
		Expectations: exp,
		ByTrace:      idx.dCounters[is].ByTrace(),
		Blamer:       idx.blamer,
	}
	return d.Calculate(nil, query, head), nil
}

// GetParamsetSummary implements the IndexSearcher interface.
func (idx *SearchIndex) GetParamsetSummary(test types.TestName, digest types.Digest, is types.IgnoreState) paramtools.ParamSet {
	return idx.paramsetSummaries[is].Get(test, digest)
}

// GetParamsetSummaryByTest implements the IndexSearcher interface.
func (idx *SearchIndex) GetParamsetSummaryByTest(is types.IgnoreState) map[types.TestName]map[types.Digest]paramtools.ParamSet {
	return idx.paramsetSummaries[is].GetByTest()
}

// GetBlame implements the IndexSearcher interface.
func (idx *SearchIndex) GetBlame(test types.TestName, digest types.Digest, commits []*tiling.Commit) blame.BlameDistribution {
	if idx.blamer == nil {
		// should never happen - indexer should have this initialized
		// before the web server starts serving requests.
		return blame.BlameDistribution{}
	}
	return idx.blamer.GetBlame(test, digest, commits)
}

type IndexerConfig struct {
	DiffStore         diff.DiffStore
	EventBus          eventbus.EventBus
	ExpectationsStore expstorage.ExpectationsStore
	GCSClient         storage.GCSClient
	TileSource        tilesource.TileSource
	Warmer            warmer.DiffWarmer
}

// Indexer is the type that continuously processes data as the underlying
// data change. It uses a DAG that encodes the dependencies of the
// different components of an index and creates a processing pipeline on top
// of it.
type Indexer struct {
	IndexerConfig

	pipeline       *pdag.Node
	indexTestsNode *pdag.Node
	lastIndex      *SearchIndex
	mutex          sync.RWMutex
}

// New returns a new IndexSource instance. It synchronously indexes the initially
// available tile. If the indexing fails an error is returned.
// The provided interval defines how often the index should be refreshed.
func New(ic IndexerConfig, interval time.Duration) (*Indexer, error) {
	ret := &Indexer{
		IndexerConfig: ic,
	}

	// Set up the processing pipeline.
	root := pdag.NewNodeWithParents(pdag.NoOp)

	// At the top level, Add the DigestCounters...
	countsNodeInclude := root.Child(calcDigestCountsInclude)
	// These are run in parallel because they can take tens of seconds
	// in large repos like Skia.
	countsNodeExclude := root.Child(calcDigestCountsExclude)

	// Node that triggers blame and writing baselines.
	// This is used to trigger when expectations change.
	// We don't need to re-calculate DigestCounts if the
	// expectations change because the DigestCounts don't care about
	// the expectations, only on the tile.
	indexTestsNode := root.Child(pdag.NoOp)

	// ... and invoke the Blamer to calculate the blames.
	blamerNode := indexTestsNode.Child(calcBlame)

	// Parameters depend on DigestCounter.
	paramsNodeInclude := pdag.NewNodeWithParents(calcParamsetsInclude, countsNodeInclude)
	// These are run in parallel because they can take tens of seconds
	// in large repos like Skia.
	paramsNodeExclude := pdag.NewNodeWithParents(calcParamsetsExclude, countsNodeExclude)

	// Write known hashes after ignores are computed. DigestCount is a
	// convenient way to get all the hashes, so that's what this node uses.
	writeHashes := countsNodeInclude.Child(writeKnownHashesList)

	// Summaries depend on DigestCounter and Blamer.
	summariesNode := pdag.NewNodeWithParents(calcSummaries, countsNodeInclude, countsNodeExclude, blamerNode)

	// The Warmer depends on summaries.
	pdag.NewNodeWithParents(runWarmer, summariesNode)

	// Set the result on the Indexer instance, once summaries, parameters and writing
	// the hash files is done.
	pdag.NewNodeWithParents(ret.setIndex, summariesNode, paramsNodeInclude, paramsNodeExclude, writeHashes)

	ret.pipeline = root
	ret.indexTestsNode = indexTestsNode

	// Process the first tile and start the indexing process.
	return ret, ret.start(interval)
}

// GetIndex implements the IndexSource interface.
func (ix *Indexer) GetIndex() IndexSearcher {
	return ix.getIndex()
}

// getIndex is like GetIndex but returns the bare struct, for
// internal package use.
func (ix *Indexer) getIndex() *SearchIndex {
	ix.mutex.RLock()
	defer ix.mutex.RUnlock()
	return ix.lastIndex
}

// start builds the initial index and starts the background
// process to continuously build indices.
func (ix *Indexer) start(interval time.Duration) error {
	if interval == 0 {
		sklog.Warning("Not starting indexer because duration was 0")
		return nil
	}
	defer shared.NewMetricsTimer("initial_synchronous_index").Stop()
	// Build the first index synchronously.
	tileStream := tilesource.GetTileStreamNow(ix.TileSource, interval, "gold-indexer")
	if err := ix.executePipeline(<-tileStream); err != nil {
		return err
	}

	// When the master expectations change, update the blamer and its dependents. This channel
	// will usually be empty, except when triaging happens. We set the size to be big enough to
	// handle a large bulk triage, if needed.
	expCh := make(chan expstorage.Delta, 100000)
	ix.EventBus.SubscribeAsync(expstorage.EV_EXPSTORAGE_CHANGED, func(e interface{}) {
		// Schedule the list of test names to be recalculated.
		expCh <- e.(*expstorage.EventExpectationChange).ExpectationDelta
	})

	// Keep building indices for different types of events. This is the central
	// event loop of the indexer.
	go func() {
		var cpxTile types.ComplexTile
		for {
			var testChanges []expstorage.Delta

			// See if there is a tile or changed tests.
			cpxTile = nil
			select {
			// Catch a new tile.
			case cpxTile = <-tileStream:
				sklog.Infof("Indexer saw a new tile")

				// Catch any test changes.
			case tn := <-expCh:
				testChanges = append(testChanges, tn)
				sklog.Infof("Indexer saw some tests change")
			}

			// Drain all input channels, effectively bunching signals together that arrive in short
			// succession.
		DrainLoop:
			for {
				select {
				case tn := <-expCh:
					testChanges = append(testChanges, tn)
				default:
					break DrainLoop
				}
			}

			// If there is a tile, re-index everything and forget the
			// individual tests that changed.
			if cpxTile != nil {
				if err := ix.executePipeline(cpxTile); err != nil {
					sklog.Errorf("Unable to index tile: %s", err)
				}
			} else if len(testChanges) > 0 {
				// Only index the tests that have changed.
				ix.indexTests(testChanges)
			}
		}
	}()

	return nil
}

// executePipeline runs the given tile through the the indexing pipeline.
// pipeline.Trigger blocks until everything is done, so this function will as well.
func (ix *Indexer) executePipeline(cpxTile types.ComplexTile) error {
	defer shared.NewMetricsTimer("indexer_execute_pipeline").Stop()
	// Create a new index from the given tile.
	sic := searchIndexConfig{
		diffStore:         ix.DiffStore,
		expectationsStore: ix.ExpectationsStore,
		gcsClient:         ix.GCSClient,
		warmer:            ix.Warmer,
	}
	return ix.pipeline.Trigger(newSearchIndex(sic, cpxTile))
}

// indexTest creates an updated index by indexing the given list of expectation changes.
func (ix *Indexer) indexTests(testChanges []expstorage.Delta) {
	// Get all the test names that had expectations changed.
	testNames := types.TestNameSet{}
	for _, d := range testChanges {
		testNames[d.Grouping] = true
	}
	if len(testNames) == 0 {
		return
	}

	sklog.Infof("Going to re-index %d tests", len(testNames))

	defer shared.NewMetricsTimer("index_tests").Stop()
	newIdx := ix.cloneLastIndex()
	// Set the testNames such that we only recompute those tests.
	newIdx.testNames = testNames
	if err := ix.indexTestsNode.Trigger(newIdx); err != nil {
		sklog.Errorf("Error indexing tests: %v \n\n Got error: %s", testNames.Keys(), err)
	}
}

// cloneLastIndex returns a copy of the most recent index.
func (ix *Indexer) cloneLastIndex() *SearchIndex {
	lastIdx := ix.getIndex()
	sic := searchIndexConfig{
		diffStore:         ix.DiffStore,
		expectationsStore: ix.ExpectationsStore,
		gcsClient:         ix.GCSClient,
		warmer:            ix.Warmer,
	}
	return &SearchIndex{
		searchIndexConfig: sic,
		cpxTile:           lastIdx.cpxTile,
		dCounters:         lastIdx.dCounters,         // stay the same even if expectations change.
		paramsetSummaries: lastIdx.paramsetSummaries, // stay the same even if expectations change.

		summaries: [2]countsAndBlames{
			// the objects inside the summaries are immutable, but may be replaced if expectations
			// are recalculated for a subset of tests.
			lastIdx.summaries[types.ExcludeIgnoredTraces],
			lastIdx.summaries[types.IncludeIgnoredTraces],
		},

		blamer: nil, // This will need to be recomputed if expectations change.

		// Force testNames to be empty, just to be sure we re-compute everything by default
		testNames: nil,
	}
}

// setIndex sets the lastIndex value at the very end of the pipeline.
func (ix *Indexer) setIndex(state interface{}) error {
	newIndex := state.(*SearchIndex)
	ix.mutex.Lock()
	defer ix.mutex.Unlock()
	ix.lastIndex = newIndex
	if ix.EventBus != nil {
		ix.EventBus.Publish(indexUpdatedEvent, state, false)
	}
	return nil
}

// calcDigestCountsInclude is the pipeline function to calculate DigestCounts from
// the full tile (not applying ignore rules)
func calcDigestCountsInclude(state interface{}) error {
	idx := state.(*SearchIndex)
	is := types.IncludeIgnoredTraces
	idx.dCounters[is] = digest_counter.New(idx.cpxTile.GetTile(is))
	return nil
}

// calcDigestCountsExclude is the pipeline function to calculate DigestCounts from
// the partial tile (applying ignore rules).
func calcDigestCountsExclude(state interface{}) error {
	idx := state.(*SearchIndex)
	is := types.ExcludeIgnoredTraces
	idx.dCounters[is] = digest_counter.New(idx.cpxTile.GetTile(is))
	return nil
}

// calcSummaries is the pipeline function to calculate the summaries.
func calcSummaries(state interface{}) error {
	idx := state.(*SearchIndex)
	exp, err := idx.expectationsStore.Get()
	if err != nil {
		return skerr.Wrap(err)
	}
	for _, is := range types.IgnoreStates {
		d := summary.Data{
			Traces:       idx.cpxTile.GetTile(is).Traces,
			Expectations: exp,
			ByTrace:      idx.dCounters[is].ByTrace(),
			Blamer:       idx.blamer,
		}
		sum := d.Calculate(idx.testNames, nil, true)
		// If we have recalculated only a subset of tests, we want to keep the results from
		// the previous scans and overwrite what we have just recomputed.
		if len(idx.testNames) > 0 && len(idx.summaries[is]) > 0 {
			idx.summaries[is] = summary.MergeSorted(idx.summaries[is], sum)
		} else {
			idx.summaries[is] = sum
		}
	}
	return nil
}

// calcParamsetsInclude is the pipeline function to calculate the parameters from
// the full tile (not applying ignore rules)
func calcParamsetsInclude(state interface{}) error {
	idx := state.(*SearchIndex)
	is := types.IncludeIgnoredTraces
	idx.paramsetSummaries[is] = paramsets.NewParamSummary(idx.cpxTile.GetTile(is), idx.dCounters[is])
	return nil
}

// calcParamsetsExclude is the pipeline function to calculate the parameters from
// the partial tile (applying ignore rules)
func calcParamsetsExclude(state interface{}) error {
	idx := state.(*SearchIndex)
	is := types.ExcludeIgnoredTraces
	idx.paramsetSummaries[is] = paramsets.NewParamSummary(idx.cpxTile.GetTile(is), idx.dCounters[is])
	return nil
}

// calcBlame is the pipeline function to calculate the blame.
func calcBlame(state interface{}) error {
	idx := state.(*SearchIndex)
	exp, err := idx.expectationsStore.Get()
	if err != nil {
		return skerr.Fmt("Could not fetch expectaions needed to calculate blame: %s", err)
	}
	b, err := blame.New(idx.cpxTile.GetTile(types.ExcludeIgnoredTraces), exp)
	if err != nil {
		idx.blamer = nil
		return skerr.Fmt("Could not calculate blame: %s", err)
	}
	idx.blamer = b
	return nil
}

func writeKnownHashesList(state interface{}) error {
	idx := state.(*SearchIndex)

	// Only write the hash file if a storage client is available.
	if idx.gcsClient == nil {
		return nil
	}

	// Trigger writing the hashes list.
	go func() {
		// Make sure this doesn't hang indefinitely. 2 minutes was chosen as a time that's plenty
		// long to make sure it completes (usually takes only a few seconds).
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		byTest := idx.DigestCountsByTest(types.IncludeIgnoredTraces)
		unavailableDigests, err := idx.diffStore.UnavailableDigests(ctx)
		if err != nil {
			sklog.Warningf("could not fetch unavailables digests, going to assume all are valid: %s", err)
			unavailableDigests = nil
		}
		// Collect all hashes in the tile that haven't been marked as unavailable yet.
		hashes := types.DigestSet{}
		for _, test := range byTest {
			for k := range test {
				if _, ok := unavailableDigests[k]; !ok {
					hashes[k] = true
				}
			}
		}

		for h := range hashes {
			if _, ok := unavailableDigests[h]; ok {
				delete(hashes, h)
			}
		}

		// Keep track of the number of known hashes since this directly affects how
		// many images the bots have to upload.
		metrics2.GetInt64Metric(knownHashesMetric).Update(int64(len(hashes)))
		if err := idx.gcsClient.WriteKnownDigests(ctx, hashes.Keys()); err != nil {
			sklog.Errorf("Error writing known digests list: %s", err)
		}
		sklog.Infof("Finished writing %d known hashes", len(hashes))
	}()
	return nil
}

// runWarmer is the pipeline function to run the warmer. It runs
// asynchronously since its results are not relevant for the searchIndex.
func runWarmer(state interface{}) error {
	idx := state.(*SearchIndex)

	is := types.IncludeIgnoredTraces
	exp, err := idx.expectationsStore.Get()
	if err != nil {
		return skerr.Wrapf(err, "preparing to run warmer - expectations failure")
	}
	d := digesttools.NewClosestDiffFinder(exp, idx.dCounters[is], idx.diffStore)

	go func() {
		// If there are somehow lots and lots of diffs or the warmer gets stuck, we should bail out
		// at some point to prevent amount of work being done on the diffstore (e.g. a remote
		// diffserver) from growing in an unbounded fashion.
		// 15 minutes was chosen based on the 90th percentile time looking at the metrics.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := idx.warmer.PrecomputeDiffs(ctx, idx.summaries[is], idx.testNames, idx.dCounters[is], d)
		if err != nil {
			sklog.Warningf("Could not precompute diffs for %d summaries and %d test names: %s", len(idx.summaries[is]), len(idx.testNames), err)
		}
	}()
	return nil
}

// Make sure SearchIndex fulfills the IndexSearcher interface
var _ IndexSearcher = (*SearchIndex)(nil)

// Make sure Indexer fulfills the IndexSource interface
var _ IndexSource = (*Indexer)(nil)
