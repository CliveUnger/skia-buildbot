package main

import (
	"encoding/json"
	"flag"
	"fmt"
	ehtml "html"
	"html/template"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

import (
	"github.com/fiorix/go-web/autogzip"
	"github.com/golang/glog"
	"github.com/rcrowley/go-metrics"
)

import (
	"skia.googlesource.com/buildbot.git/perf/go/alerting"
	"skia.googlesource.com/buildbot.git/perf/go/annotate"
	"skia.googlesource.com/buildbot.git/perf/go/clustering"
	"skia.googlesource.com/buildbot.git/perf/go/config"
	"skia.googlesource.com/buildbot.git/perf/go/db"
	"skia.googlesource.com/buildbot.git/perf/go/filetilestore"
	"skia.googlesource.com/buildbot.git/perf/go/flags"
	"skia.googlesource.com/buildbot.git/perf/go/gitinfo"
	"skia.googlesource.com/buildbot.git/perf/go/human"
	"skia.googlesource.com/buildbot.git/perf/go/login"
	"skia.googlesource.com/buildbot.git/perf/go/parser"
	"skia.googlesource.com/buildbot.git/perf/go/shortcut"
	"skia.googlesource.com/buildbot.git/perf/go/stats"
	"skia.googlesource.com/buildbot.git/perf/go/trybot"
	"skia.googlesource.com/buildbot.git/perf/go/types"
	"skia.googlesource.com/buildbot.git/perf/go/util"
	"skia.googlesource.com/buildbot.git/perf/go/vec"
)

var (
	// indexTemplate is the main index.html page we serve.
	indexTemplate *template.Template = nil

	// clusterTemplate is the /clusters/ page we serve.
	clusterTemplate *template.Template = nil

	alertsTemplate *template.Template = nil

	clTemplate *template.Template = nil

	helpTemplate *template.Template = nil

	// compareTemplate is the /compare/ page we serve.
	compareTemplate *template.Template = nil

	jsonHandlerPath = regexp.MustCompile(`/json/([a-z]*)$`)

	shortcutHandlerPath = regexp.MustCompile(`/shortcuts/([0-9]*)$`)

	// The three capture groups are dataset, tile scale, and tile number.
	tileHandlerPath = regexp.MustCompile(`/tiles/([0-9]*)/([-0-9]*)/$`)

	// The optional capture group is a githash.
	singleHandlerPath = regexp.MustCompile(`/single/([0-9a-f]+)?$`)

	// The three capture groups are tile scale, tile number, and an optional 'trace.
	queryHandlerPath = regexp.MustCompile(`/query/([0-9]*)/([-0-9]*)/(traces/)?$`)

	clHandlerPath = regexp.MustCompile(`/cl/([0-9]*)$`)

	git *gitinfo.GitInfo = nil

	commitLinkifyRe = regexp.MustCompile("(?m)^commit (.*)$")
)

// flags
var (
	port           = flag.String("port", ":8000", "HTTP service address (e.g., ':8000')")
	local          = flag.Bool("local", false, "Running locally if true. As opposed to in production.")
	gitRepoDir     = flag.String("git_repo_dir", "../../../skia", "Directory location for the Skia repo.")
	tileStoreDir   = flag.String("tile_store_dir", "/tmp/tileStore", "What directory to look for tilebuilder tiles in.")
	graphiteServer = flag.String("graphite_server", "skia-monitoring-b:2003", "Where is Graphite metrics ingestion server running.")
	apikey         = flag.String("apikey", "", "The API Key used to make issue tracker requests. Only for local testing.")
)

var (
	nanoTileStore types.TileStore
)

func Init() {
	rand.Seed(time.Now().UnixNano())

	metrics.RegisterRuntimeMemStats(metrics.DefaultRegistry)
	go metrics.CaptureRuntimeMemStats(metrics.DefaultRegistry, 1*time.Minute)
	addr, _ := net.ResolveTCPAddr("tcp", *graphiteServer)
	go metrics.Graphite(metrics.DefaultRegistry, 1*time.Minute, "skiaperf", addr)

	// Change the current working directory to two directories up from this source file so that we
	// can read templates and serve static (res/) files.
	_, filename, _, _ := runtime.Caller(0)
	cwd := filepath.Join(filepath.Dir(filename), "../..")
	if err := os.Chdir(cwd); err != nil {
		glog.Fatalln(err)
	}

	indexTemplate = template.Must(template.ParseFiles(
		filepath.Join(cwd, "templates/index.html"),
		filepath.Join(cwd, "templates/titlebar.html"),
		filepath.Join(cwd, "templates/header.html"),
	))
	clusterTemplate = template.Must(template.ParseFiles(
		filepath.Join(cwd, "templates/clusters.html"),
		filepath.Join(cwd, "templates/titlebar.html"),
		filepath.Join(cwd, "templates/header.html"),
	))
	alertsTemplate = template.Must(template.ParseFiles(
		filepath.Join(cwd, "templates/alerting.html"),
		filepath.Join(cwd, "templates/titlebar.html"),
		filepath.Join(cwd, "templates/header.html"),
	))
	clTemplate = template.Must(template.ParseFiles(
		filepath.Join(cwd, "templates/cl.html"),
		filepath.Join(cwd, "templates/titlebar.html"),
		filepath.Join(cwd, "templates/header.html"),
	))
	compareTemplate = template.Must(template.ParseFiles(
		filepath.Join(cwd, "templates/compare.html"),
		filepath.Join(cwd, "templates/titlebar.html"),
		filepath.Join(cwd, "templates/header.html"),
	))
	helpTemplate = template.Must(template.ParseFiles(
		filepath.Join(cwd, "templates/help.html"),
		filepath.Join(cwd, "templates/titlebar.html"),
		filepath.Join(cwd, "templates/header.html"),
	))

	nanoTileStore = filetilestore.NewFileTileStore(*tileStoreDir, "nano", 2*time.Minute)

	var err error
	git, err = gitinfo.NewGitInfo(*gitRepoDir, true)
	if err != nil {
		glog.Fatal(err)
	}
}

// showcutHandler handles the POST requests of the shortcut page.
//
// Shortcuts are of the form:
//
//    {
//       "scale": 0,
//       "tiles": [-1],
//       "hash": "a1092123890...",
//       "ids": [
//            "x86:...",
//            "x86:...",
//            "x86:...",
//       ]
//    }
//
// hash - The git hash of where a step was detected. Can be null.
//
func shortcutHandler(w http.ResponseWriter, r *http.Request) {
	// TODO(jcgregorio): Add unit tests.
	match := shortcutHandlerPath.FindStringSubmatch(r.URL.Path)
	if match == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == "POST" {
		// check header
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			util.ReportError(w, r, fmt.Errorf("Error: received %s", ct), "Invalid content type.")
			return
		}
		defer r.Body.Close()
		id, err := shortcut.Insert(r.Body)
		if err != nil {
			util.ReportError(w, r, err, "Error inserting shortcut.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		if err := enc.Encode(map[string]string{"id": id}); err != nil {
			util.ReportError(w, r, err, "Error while encoding response.")
		}
	} else {
		http.NotFound(w, r)
	}
}

// trybotHandler handles the GET for trybot data.
func trybotHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Trybot Handler: %q\n", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	try, err := trybot.List(50)
	if err != nil {
		util.ReportError(w, r, err, "Failed to retrieve trybot results.")
		return
	}
	enc := json.NewEncoder(w)
	if err = enc.Encode(try); err != nil {
		util.ReportError(w, r, err, "Error while encoding response.")
	}
}

// alertsHandler serves the HTML for the /alerts/ page.
//
// See alertingHandler for the JSON it uses.
func alertsHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Alerts Handler: %q\n", r.URL.Path)
	w.Header().Set("Content-Type", "text/html")
	if err := alertsTemplate.Execute(w, nil); err != nil {
		glog.Errorln("Failed to expand template:", err)
	}
}

// alertingHandler returns the currently untriaged clusters.
//
// The return format is the same as clusteringHandler.
func alertingHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Alerting Handler: %q\n", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	tile, err := nanoTileStore.Get(0, -1)
	if err != nil {
		util.ReportError(w, r, err, fmt.Sprintf("Failed to load tile."))
		return
	}

	alerts, err := alerting.ListFrom(tile.Commits[0].CommitTime)
	if err != nil {
		util.ReportError(w, r, err, "Error retrieving cluster summaries.")
		return
	}
	enc := json.NewEncoder(w)
	if err = enc.Encode(map[string][]*types.ClusterSummary{"Clusters": alerts}); err != nil {
		util.ReportError(w, r, err, "Error while encoding response.")
	}
}

// clHandler serves the HTML for the /cl/<id> page.
//
// These are shortcuts to individual clusters.
//
// See alertingHandler for the JSON it uses.
//
func clHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	match := clHandlerPath.FindStringSubmatch(r.URL.Path)
	if r.Method != "GET" || match == nil || len(match) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(match[1], 10, 0)
	if err != nil {
		util.ReportError(w, r, err, "Failed parsing ID.")
		return
	}
	cl, err := alerting.Get(id)
	if err != nil {
		util.ReportError(w, r, err, "Failed to find cluster with that ID.")
		return
	}
	if err := clTemplate.Execute(w, cl); err != nil {
		glog.Errorln("Failed to expand template:", err)
	}
}

// compareHandler handles the GET of the compare page.
func compareHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Compare Handler: %q\n", r.URL.Path)
	w.Header().Set("Content-Type", "text/html")
	if err := compareTemplate.Execute(w, nil); err != nil {
		glog.Errorln("Failed to expand template:", err)
	}
}

// clustersHandler handles the GET of the clusters page.
func clustersHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Cluster Handler: %q\n", r.URL.Path)
	w.Header().Set("Content-Type", "text/html")
	if err := clusterTemplate.Execute(w, nil); err != nil {
		glog.Errorln("Failed to expand template:", err)
	}
}

// writeClusterSummaries writes out a ClusterSummaries instance as a JSON response.
func writeClusterSummaries(summary *clustering.ClusterSummaries, w http.ResponseWriter, r *http.Request) {
	enc := json.NewEncoder(w)
	if err := enc.Encode(summary); err != nil {
		util.ReportError(w, r, err, "Error while encoding ClusterSummaries response.")
	}
}

// clusteringHandler handles doing the actual k-means clustering.
//
// The return format is JSON of the form:
//
// {
//   "Clusters": [
//     {
//       "Keys": [
//          "x86:GeForce320M:MacMini4.1:Mac10.8:GM_varied_text_clipped_no_lcd_640_480:8888",...],
//       "ParamSummaries": [
//           [{"Value": "Win8", "Weight": 15}, {"Value": "Android", "Weight": 14}, ...]
//       ],
//       "StepFit": {
//          "LeastSquares":0.0006582442047814354,
//          "TurningPoint":162,
//          "StepSize":0.023272272692293046,
//          "Regression": 35.3
//       }
//       Traces: [[[0, -0.00007967326606768456], [1, 0.011877665949459049], [2, 0.012158129176717419],...]]
//     },
//     ...
//   ],
//   "K": 5,
//   "StdDevThreshhold": 0.1
// }
//
// Note that Keys contains all the keys, while Traces only contains traces of
// the N closest cluster members and the centroid.
//
// Takes the following query parameters:
//
//   _k      - The K to use for k-means clustering.
//   _stddev - The standard deviation to use when normalize traces
//             during k-means clustering.
//   _issue  - The Rietveld issue ID with trybot results to include.
//
// Additionally the rest of the query parameters as returned from
// sk.Query.selectionsAsQuery().
func clusteringHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Clustering Handler: %q\n", r.URL.Path)
	tile, err := nanoTileStore.Get(0, -1)
	if err != nil {
		util.ReportError(w, r, err, fmt.Sprintf("Failed to load tile."))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// If there are no query parameters just return with an empty set of ClusterSummaries.
	if r.FormValue("_k") == "" || r.FormValue("_stddev") == "" {
		writeClusterSummaries(clustering.NewClusterSummaries(), w, r)
		return
	}

	k, err := strconv.ParseInt(r.FormValue("_k"), 10, 32)
	if err != nil {
		util.ReportError(w, r, err, fmt.Sprintf("_k parameter must be an integer %s.", r.FormValue("_k")))
		return
	}
	stddev, err := strconv.ParseFloat(r.FormValue("_stddev"), 64)
	if err != nil {
		util.ReportError(w, r, err, fmt.Sprintf("_stddev parameter must be a float %s.", r.FormValue("_stddev")))
		return
	}

	issue := r.FormValue("_issue")
	var tryResults *types.TryBotResults = nil
	if issue != "" {
		var err error
		tryResults, err = trybot.Get(issue)
		if err != nil {
			util.ReportError(w, r, err, fmt.Sprintf("Failed to get trybot data for clustering."))
			return
		}
	}

	delete(r.Form, "_k")
	delete(r.Form, "_stddev")
	delete(r.Form, "_issue")

	// Create a filter function for traces that match the query parameters and
	// optionally tryResults.
	filter := func(key string, tr *types.PerfTrace) bool {
		if tryResults != nil {
			if _, ok := tryResults.Values[key]; !ok {
				return false
			}
		}
		return types.Matches(tr, r.Form)
	}

	if issue != "" {
		if tile, err = trybot.TileWithTryData(tile, issue); err != nil {
			util.ReportError(w, r, err, fmt.Sprintf("Failed to get trybot data for clustering."))
			return
		}
	}
	summary, err := clustering.CalculateClusterSummaries(tile, int(k), stddev, filter)
	if err != nil {
		util.ReportError(w, r, err, "Failed to calculate clusters.")
		return
	}
	writeClusterSummaries(summary, w, r)
}

// getTile retrieves a tile from the disk
func getTile(tileScale, tileNumber int) (*types.Tile, error) {
	start := time.Now()
	tile, err := nanoTileStore.Get(int(tileScale), int(tileNumber))
	glog.Infoln("Time for tile load: ", time.Since(start).Nanoseconds())
	if err != nil || tile == nil {
		return nil, fmt.Errorf("Unable to get tile from tilestore: ", err)
	}
	return tile, nil
}

// tileHandler accepts URIs like /tiles/0/1
// where the URI format is /tiles/<tile-scale>/<tile-number>
//
// It returns JSON of the form:
//
//  {
//    tiles: [20],
//    scale: 0,
//    paramset: {
//      "os": ["Android", "ChromeOS", ..],
//      "arch": ["Arm7", "x86", ...],
//    },
//    commits: [
//      {
//        "commit_time": 140329432,
//        "hash": "0e03478100ea",
//        "author": "someone@google.com",
//        "commit_msg": "The subject line of the commit.",
//      },
//      ...
//    ],
//    ticks: [
//      [1.5, "Mon"],
//      [3.5, "Tue"]
//    ],
//    skps: [
//      5, 13, 24
//    ]
//  }
//
//  Where skps are the commit indices where the SKPs were updated.
//
func tileHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Tile Handler: %q\n", r.URL.Path)
	handlerStart := time.Now()
	match := tileHandlerPath.FindStringSubmatch(r.URL.Path)
	if r.Method != "GET" || match == nil || len(match) != 3 {
		http.NotFound(w, r)
		return
	}
	tileScale, err := strconv.ParseInt(match[1], 10, 0)
	if err != nil {
		util.ReportError(w, r, err, "Failed parsing tile scale.")
		return
	}
	tileNumber, err := strconv.ParseInt(match[2], 10, 0)
	if err != nil {
		util.ReportError(w, r, err, "Failed parsing tile number.")
		return
	}
	glog.Infof("tile: %d %d", tileScale, tileNumber)
	tile, err := getTile(int(tileScale), int(tileNumber))
	if err != nil {
		util.ReportError(w, r, err, "Failed retrieving tile.")
		return
	}

	guiTile := types.NewTileGUI(tile.Scale, tile.TileIndex)
	guiTile.Commits = tile.Commits
	guiTile.ParamSet = tile.ParamSet
	// SkpCommits goes out to the git repo, add caching if this turns out to be
	// slow.
	if skps, err := git.SkpCommits(tile); err != nil {
		guiTile.Skps = []int{}
		glog.Errorf("Failed to calculate skps: %s", err)
	} else {
		guiTile.Skps = skps
	}

	ts := []int64{}
	for _, c := range tile.Commits {
		if c.CommitTime != 0 {
			ts = append(ts, c.CommitTime)
		}
	}
	glog.Infof("%#v", ts)
	guiTile.Ticks = human.FlotTickMarks(ts)

	// Marshal and send
	marshaledResult, err := json.Marshal(guiTile)
	if err != nil {
		util.ReportError(w, r, err, "Failed to marshal JSON.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(marshaledResult)
	if err != nil {
		util.ReportError(w, r, err, "Error while marshalling results.")
	}
	glog.Infoln("Total handler time: ", time.Since(handlerStart).Nanoseconds())
}

// QueryResponse is for formatting the JSON output from queryHandler.
type QueryResponse struct {
	Traces []*types.TraceGUI `json:"traces"`
	Hash   string            `json:"hash"`
}

// FlatQueryResponse is for formatting the JSON output from calcHandler when the user
// requests flat=true. The output isn't formatted for input into Flot, instead the Values
// are returned as a simple slice, which is easier to work with in IPython.
type FlatQueryResponse struct {
	Traces []*types.PerfTrace
}

// queryHandler handles queries for and about traces.
//
// Queries look like:
//
//     /query/0/-1/?arch=Arm7&arch=x86&scale=1
//
// Where they keys and values in the query params are from the ParamSet.
// Repeated parameters are matched via OR. I.e. the above query will include
// anything that has an arch of Arm7 or x86.
//
// The first two path paramters are tile scale and tile number, where -1 means
// the last tile at the given scale.
//
// The normal response is JSON of the form:
//
// {
//   "matches": 187,
// }
//
// If the path is:
//
//    /query/0/-1/traces/?arch=Arm7&arch=x86&scale=1
//
// Then the response is the set of traces that match that query.
//
//  {
//    "traces": [
//      {
//        // All of these keys and values should be exactly what Flot consumes.
//        data: [[1, 1.1], [20, 30]],
//        label: "key1",
//        _params: {"os: "Android", ...}
//      },
//      {
//        data: [[1.2, 2.1], [20, 35]],
//        label: "key2",
//        _params: {"os: "Android", ...}
//      }
//    ]
//  }
//
// If the path is:
//
//    /query/0/-1/traces/?__shortcut=11
//
// Then the traces in the shortcut with that ID are returned, along with the
// git hash at the step function, if the shortcut came from an alert.
//
//  {
//    "traces": [
//      {
//        // All of these keys and values should be exactly what Flot consumes.
//        data: [[1, 1.1], [20, 30]],
//        label: "key1",
//        _params: {"os: "Android", ...}
//      },
//      ...
//    ],
//    "hash": "a012334...",
//  }
//
//
// TODO Add ability to query across a range of tiles.
func queryHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Query Handler: %q\n", r.URL.Path)
	match := queryHandlerPath.FindStringSubmatch(r.URL.Path)
	glog.Infof("%#v", match)
	if r.Method != "GET" || match == nil || len(match) != 4 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		util.ReportError(w, r, err, "Failed to parse query params.")
	}
	tileScale, err := strconv.ParseInt(match[1], 10, 0)
	if err != nil {
		util.ReportError(w, r, err, "Failed parsing tile scale.")
		return
	}
	tileNumber, err := strconv.ParseInt(match[2], 10, 0)
	if err != nil {
		util.ReportError(w, r, err, "Failed parsing tile number.")
		return
	}
	glog.Infof("tile: %d %d", tileScale, tileNumber)
	tile, err := getTile(int(tileScale), int(tileNumber))
	if err != nil {
		util.ReportError(w, r, err, "Failed retrieving tile.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	ret := &QueryResponse{
		Traces: []*types.TraceGUI{},
		Hash:   "",
	}
	if match[3] == "" {
		// We only want the count.
		total := 0
		for _, tr := range tile.Traces {
			if types.Matches(tr, r.Form) {
				total++
			}
		}
		glog.Info("Count: ", total)
		inc := json.NewEncoder(w)
		if err := inc.Encode(map[string]int{"matches": total}); err != nil {
			util.ReportError(w, r, err, "Error while encoding query response.")
			return
		}
	} else {
		// We want the matching traces.
		shortcutID := r.Form.Get("__shortcut")
		if shortcutID != "" {
			sh, err := shortcut.Get(shortcutID)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if sh.Issue != "" {
				if tile, err = trybot.TileWithTryData(tile, sh.Issue); err != nil {
					util.ReportError(w, r, err, "Failed to populate shortcut data with trybot result.")
					return
				}
			}
			ret.Hash = sh.Hash
			for _, k := range sh.Keys {
				if tr, ok := tile.Traces[k]; ok {
					tg := traceGuiFromTrace(tr.(*types.PerfTrace), k, tile)
					if tg != nil {
						ret.Traces = append(ret.Traces, tg)
					}
				} else if types.IsFormulaID(k) {
					// Re-evaluate the formula and add all the results to the response.
					formula := types.FormulaFromID(k)
					if err := addCalculatedTraces(ret, tile, formula); err != nil {
						glog.Errorf("Failed evaluating formula (%q) while processing shortcut %s: %s", formula, shortcutID, err)
					}
				} else if strings.HasPrefix(k, "!") {
					glog.Errorf("A calculated trace is slipped through: (%s) in shortcut %s: %s", k, shortcutID, err)
				}
			}
		} else {
			for key, tr := range tile.Traces {
				if types.Matches(tr, r.Form) {
					tg := traceGuiFromTrace(tr.(*types.PerfTrace), key, tile)
					if tg != nil {
						ret.Traces = append(ret.Traces, tg)
					}
				}
			}
		}
		enc := json.NewEncoder(w)
		if err := enc.Encode(ret); err != nil {
			util.ReportError(w, r, err, "Error while encoding query response.")
			return
		}
	}
}

// SingleTrace is used in SingleResponse.
type SingleTrace struct {
	Val    float64           `json:"val"`
	Params map[string]string `json:"params"`
}

// SingleResponse is for formatting the JSON output from singleHandler.
// Hash is the commit hash whose data are used in Traces.
type SingleResponse struct {
	Traces []*SingleTrace `json:"traces"`
	Hash   string         `json:"hash"`
}

// singleHandler is similar to /query/0/-1/traces?<param filters>, but takes an
// optional commit hash and returns a single value for each trace at that commit,
// or the latest value if a hash is not given or found. The resulting JSON is in
// SingleResponse format that looks like:
//
//  {
//    "traces": [
//      {
//        val: 1.1,
//        params: {"os: "Android", ...}
//      },
//      ...
//    ],
//    "hash": "abc123",
//  }
//
func singleHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Single Handler: %q\n", r.URL.Path)
	handlerStart := time.Now()
	match := singleHandlerPath.FindStringSubmatch(r.URL.Path)
	if r.Method != "GET" || match == nil || len(match) != 2 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		util.ReportError(w, r, err, "Failed to parse query params.")
	}
	hash := match[1]

	tileNum, idx, err := git.TileAddressFromHash(hash, time.Time(config.BEGINNING_OF_TIME))
	if err != nil {
		glog.Infof("Did not find hash '%s', use latest: %q.\n", hash, err)
		tileNum = -1
		idx = -1
	}
	glog.Infof("Hash: %s tileNum: %d, idx: %d\n", hash, tileNum, idx)
	tile, err := getTile(0, tileNum)
	if err != nil {
		util.ReportError(w, r, err, "Failed retrieving tile.")
		return
	}

	if idx < 0 {
		idx = len(tile.Commits) - 1 // Defaults to the last slice element.
	}
	glog.Infof("Tile: %d; Idx: %d\n", tileNum, idx)

	ret := SingleResponse{
		Traces: []*SingleTrace{},
		Hash:   tile.Commits[idx].Hash,
	}
	for _, tr := range tile.Traces {
		if types.Matches(tr, r.Form) {
			v, err := vec.FillAt(tr.(*types.PerfTrace).Values, idx)
			if err != nil {
				util.ReportError(w, r, err, "Error while getting value at slice index.")
				return
			}
			t := &SingleTrace{
				Val:    v,
				Params: tr.Params(),
			}
			ret.Traces = append(ret.Traces, t)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(ret); err != nil {
		util.ReportError(w, r, err, "Error while encoding single results.")
	}
	glog.Infoln("Total handler time: ", time.Since(handlerStart).Nanoseconds())
}

// traceGuiFromTrace returns a populated TraceGUI from the given trace.
func traceGuiFromTrace(trace *types.PerfTrace, key string, tile *types.Tile) *types.TraceGUI {
	newTraceData := make([][2]float64, 0)
	for i, v := range trace.Values {
		if v != config.MISSING_DATA_SENTINEL && tile.Commits[i] != nil && tile.Commits[i].CommitTime > 0 {
			//newTraceData = append(newTraceData, [2]float64{float64(tile.Commits[i].CommitTime), v})
			newTraceData = append(newTraceData, [2]float64{float64(i), v})
		}
	}
	if len(newTraceData) >= 0 {
		return &types.TraceGUI{
			Data:   newTraceData,
			Label:  key,
			Params: trace.Params(),
		}
	} else {
		return nil
	}
}

// addCalculatedTraces adds the traces returned from evaluating the given
// formula over the given tile to the QueryResponse.
func addCalculatedTraces(qr *QueryResponse, tile *types.Tile, formula string) error {
	ctx := parser.NewContext(tile)
	traces, err := ctx.Eval(formula)
	if err != nil {
		return fmt.Errorf("Failed to evaluate formula %q: %s", formula, err)
	}
	hasFormula := false
	for _, tr := range traces {
		if types.IsFormulaID(tr.Params()["id"]) {
			hasFormula = true
		}
		tg := traceGuiFromTrace(tr, tr.Params()["id"], tile)
		qr.Traces = append(qr.Traces, tg)
	}
	if !hasFormula {
		// If we haven't added the formula trace to the response yet, add it in now.
		f := types.NewPerfTraceN(len(tile.Commits))
		tg := traceGuiFromTrace(f, types.AsFormulaID(formula), tile)
		qr.Traces = append(qr.Traces, tg)
	}
	return nil
}

// addFlatCalculatedTraces adds the traces returned from evaluating the given
// formula over the given tile to the FlatQueryResponse. Doesn't include an empty
// formula trace. Useful for pulling data into IPython.
func addFlatCalculatedTraces(qr *FlatQueryResponse, tile *types.Tile, formula string) error {
	ctx := parser.NewContext(tile)
	traces, err := ctx.Eval(formula)
	if err != nil {
		return fmt.Errorf("Failed to evaluate formula %q: %s", formula, err)
	}
	for _, tr := range traces {
		qr.Traces = append(qr.Traces, tr)
	}
	return nil
}

// calcHandler handles requests for the form:
//
//    /calc/?formula=filter("config=8888")
//
// Where the formula is any formula that parser.Eval() accepts.
//
// The response is the same format as queryHandler.
func calcHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Calc Handler: %q\n", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	tile, err := nanoTileStore.Get(0, -1)
	if err != nil {
		util.ReportError(w, r, err, fmt.Sprintf("Failed to load tile."))
		return
	}
	formula := r.FormValue("formula")

	var data interface{} = nil
	if r.FormValue("flat") == "true" {
		resp := &FlatQueryResponse{
			Traces: []*types.PerfTrace{},
		}
		if err := addFlatCalculatedTraces(resp, tile, formula); err != nil {
			util.ReportError(w, r, err, fmt.Sprintf("Failed in /calc/ to evaluate formula."))
			return
		}
		data = resp
	} else {
		resp := &QueryResponse{
			Traces: []*types.TraceGUI{},
			Hash:   "",
		}
		if err := addCalculatedTraces(resp, tile, formula); err != nil {
			util.ReportError(w, r, err, fmt.Sprintf("Failed in /calc/ to evaluate formula."))
			return
		}
		data = resp
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(data); err != nil {
		util.ReportError(w, r, err, "Error while encoding query response.")
		return
	}
}

// commitsHandler handles requests for commits.
//
// Queries look like:
//
//     /commits/?begin=hash1&end=hash2
//
//  or if there is only one hash:
//
//     /commits/?begin=hash
//
// The response is HTML of the form:
//
//  <pre>
//    commit <a href="http://skia.googlesource....">5bdbd13d8833d23e0da552f6817ae0b5a4e849e5</a>
//    Author: Joe Gregorio <jcgregorio@google.com>
//    Date:   Wed Aug 6 16:16:18 2014 -0400
//
//        Back to plotting lines.
//
//        perf/go/skiaperf/perf.go
//        perf/go/types/types.go
//        perf/res/js/logic.js
//
//    commit <a
//    ...
//  </pre>
//
func commitsHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Query Handler: %q\n", r.URL.Path)
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	begin := r.FormValue("begin")
	if len(begin) != 40 {
		util.ReportError(w, r, fmt.Errorf("Invalid hash format: %s", begin), "Error while looking up hashes.")
		return
	}
	end := r.FormValue("end")
	body, err := git.Log(begin, end)
	if err != nil {
		util.ReportError(w, r, err, "Error while looking up hashes.")
		return
	}
	escaped := ehtml.EscapeString(body)
	linkified := commitLinkifyRe.ReplaceAllString(escaped, "<span class=subject>commit <a href=\"https://skia.googlesource.com/skia/+/${1}\" target=\"_blank\">${1}</a></span>")

	w.Write([]byte(fmt.Sprintf("<pre>%s</pre>", linkified)))
}

// shortCommitsHandler returns basic info of a range of commits.
//
// Queries look like:
//
//     /commits/?begin=hash1&end=hash2
//
// Response is JSON of ShortCommits format that looks like:
//
// {
//   "commits": [
//     {
//       hash: "123abc",
//       author: "bensong",
//       subject: "Adds short commits."
//     },
//     ...
//   ]
// }
//
func shortCommitsHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Query Handler: %q\n", r.URL.Path)
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	begin := r.FormValue("begin")
	if len(begin) != 40 {
		util.ReportError(w, r, fmt.Errorf("Invalid begin hash format: %s", begin), "Error while looking up hashes.")
		return
	}
	end := r.FormValue("end")
	if len(end) != 40 {
		util.ReportError(w, r, fmt.Errorf("Invalid end hash format: %s", end), "Error while looking up hashes.")
		return
	}
	commits, err := git.ShortList(begin, end)
	if err != nil {
		util.ReportError(w, r, err, "Error while looking up hashes.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(commits); err != nil {
		util.ReportError(w, r, err, "Error while encoding response.")
	}
}

// mainHandler handles the GET of the main page.
func mainHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Main Handler: %q\n", r.URL.Path)
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html")
		if err := indexTemplate.Execute(w, struct{}{}); err != nil {
			glog.Errorln("Failed to expand template:", err)
		}
	}
}

// helpHandler handles the GET of the main page.
func helpHandler(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Help Handler: %q\n", r.URL.Path)
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html")
		ctx := parser.NewContext(nil)
		if err := helpTemplate.Execute(w, ctx); err != nil {
			glog.Errorln("Failed to expand template:", err)
		}
	}
}

func makeResourceHandler() func(http.ResponseWriter, *http.Request) {
	fileServer := http.FileServer(http.Dir("./"))
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", string(300))
		fileServer.ServeHTTP(w, r)
	}
}

func main() {
	flag.Parse()
	flags.Log()

	Init()
	db.Init()
	stats.Start(nanoTileStore, git)
	alerting.Start(nanoTileStore, *apikey)
	login.Init(*local)
	glog.Infoln("Begin loading data.")

	// Resources are served directly.
	http.HandleFunc("/res/", autogzip.HandleFunc(makeResourceHandler()))

	http.HandleFunc("/", autogzip.HandleFunc(mainHandler))
	http.HandleFunc("/shortcuts/", shortcutHandler)
	http.HandleFunc("/tiles/", tileHandler)
	http.HandleFunc("/single/", singleHandler)
	http.HandleFunc("/query/", queryHandler)
	http.HandleFunc("/commits/", commitsHandler)
	http.HandleFunc("/shortcommits/", shortCommitsHandler)
	http.HandleFunc("/trybots/", autogzip.HandleFunc(trybotHandler))
	http.HandleFunc("/clusters/", autogzip.HandleFunc(clustersHandler))
	http.HandleFunc("/clustering/", autogzip.HandleFunc(clusteringHandler))
	http.HandleFunc("/cl/", autogzip.HandleFunc(clHandler))
	http.HandleFunc("/alerts/", autogzip.HandleFunc(alertsHandler))
	http.HandleFunc("/alerting/", autogzip.HandleFunc(alertingHandler))
	http.HandleFunc("/annotate/", autogzip.HandleFunc(annotate.Handler))
	http.HandleFunc("/compare/", autogzip.HandleFunc(compareHandler))
	http.HandleFunc("/calc/", autogzip.HandleFunc(calcHandler))
	http.HandleFunc("/help/", autogzip.HandleFunc(helpHandler))
	http.HandleFunc("/oauth2callback/", login.OAuth2CallbackHandler)
	http.HandleFunc("/logout/", login.LogoutHandler)
	http.HandleFunc("/loginstatus/", login.StatusHandler)

	glog.Infoln("Ready to serve.")
	glog.Fatal(http.ListenAndServe(*port, nil))
}
