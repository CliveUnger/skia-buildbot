/*
	Handlers and types specific to Chromium perf tasks.
*/

package chromium_perf

import (
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/gorilla/mux"

	"go.skia.org/infra/ct/go/ctfe/task_common"
	ctfeutil "go.skia.org/infra/ct/go/ctfe/util"
	"go.skia.org/infra/ct/go/db"
	ctutil "go.skia.org/infra/ct/go/util"
)

var (
	addTaskTemplate     *template.Template = nil
	runsHistoryTemplate *template.Template = nil
)

func ReloadTemplates(resourcesDir string) {
	addTaskTemplate = template.Must(template.ParseFiles(
		filepath.Join(resourcesDir, "templates/chromium_perf.html"),
		filepath.Join(resourcesDir, "templates/header.html"),
		filepath.Join(resourcesDir, "templates/titlebar.html"),
	))
	runsHistoryTemplate = template.Must(template.ParseFiles(
		filepath.Join(resourcesDir, "templates/chromium_perf_runs_history.html"),
		filepath.Join(resourcesDir, "templates/header.html"),
		filepath.Join(resourcesDir, "templates/titlebar.html"),
	))
}

type DBTask struct {
	task_common.CommonCols

	Benchmark            string         `db:"benchmark"`
	Platform             string         `db:"platform"`
	PageSets             string         `db:"page_sets"`
	CustomWebpages       string         `db:"custom_webpages"`
	RepeatRuns           int64          `db:"repeat_runs"`
	RunInParallel        bool           `db:"run_in_parallel"`
	BenchmarkArgs        string         `db:"benchmark_args"`
	BrowserArgsNoPatch   string         `db:"browser_args_nopatch"`
	BrowserArgsWithPatch string         `db:"browser_args_withpatch"`
	Description          string         `db:"description"`
	ChromiumPatch        string         `db:"chromium_patch"`
	BlinkPatch           string         `db:"blink_patch"`
	SkiaPatch            string         `db:"skia_patch"`
	CatapultPatch        string         `db:"catapult_patch"`
	BenchmarkPatch       string         `db:"benchmark_patch"`
	Results              sql.NullString `db:"results"`
	NoPatchRawOutput     sql.NullString `db:"nopatch_raw_output"`
	WithPatchRawOutput   sql.NullString `db:"withpatch_raw_output"`
}

func (task DBTask) GetTaskName() string {
	return "ChromiumPerf"
}

func (dbTask DBTask) GetPopulatedAddTaskVars() task_common.AddTaskVars {
	taskVars := &AddTaskVars{}
	taskVars.Username = dbTask.Username
	taskVars.TsAdded = ctutil.GetCurrentTs()
	taskVars.RepeatAfterDays = strconv.FormatInt(dbTask.RepeatAfterDays, 10)
	taskVars.Benchmark = dbTask.Benchmark
	taskVars.Platform = dbTask.Platform
	taskVars.PageSets = dbTask.PageSets
	taskVars.CustomWebpages = dbTask.CustomWebpages
	taskVars.RepeatRuns = strconv.FormatInt(dbTask.RepeatRuns, 10)
	taskVars.RunInParallel = strconv.FormatBool(dbTask.RunInParallel)
	taskVars.BenchmarkArgs = dbTask.BenchmarkArgs
	taskVars.BrowserArgsNoPatch = dbTask.BrowserArgsNoPatch
	taskVars.BrowserArgsWithPatch = dbTask.BrowserArgsWithPatch
	taskVars.Description = dbTask.Description
	taskVars.ChromiumPatch = dbTask.ChromiumPatch
	taskVars.BlinkPatch = dbTask.BlinkPatch
	taskVars.SkiaPatch = dbTask.SkiaPatch
	taskVars.CatapultPatch = dbTask.CatapultPatch
	taskVars.BenchmarkPatch = dbTask.BenchmarkPatch
	return taskVars
}

func (task DBTask) GetResultsLink() string {
	if task.Results.Valid {
		return task.Results.String
	} else {
		return ""
	}
}

func (task DBTask) GetUpdateTaskVars() task_common.UpdateTaskVars {
	return &UpdateVars{}
}

func (task DBTask) RunsOnGCEWorkers() bool {
	// Perf tasks should always run on bare-metal machines.
	return false
}

func (task DBTask) TableName() string {
	return db.TABLE_CHROMIUM_PERF_TASKS
}

func (task DBTask) Select(query string, args ...interface{}) (interface{}, error) {
	result := []DBTask{}
	err := db.DB.Select(&result, query, args...)
	return result, err
}

func addTaskView(w http.ResponseWriter, r *http.Request) {
	ctfeutil.ExecuteSimpleTemplate(addTaskTemplate, w, r)
}

type AddTaskVars struct {
	task_common.AddTaskCommonVars

	Benchmark            string `json:"benchmark"`
	Platform             string `json:"platform"`
	PageSets             string `json:"page_sets"`
	CustomWebpages       string `json:"custom_webpages"`
	RepeatRuns           string `json:"repeat_runs"`
	RunInParallel        string `json:"run_in_parallel"`
	BenchmarkArgs        string `json:"benchmark_args"`
	BrowserArgsNoPatch   string `json:"browser_args_nopatch"`
	BrowserArgsWithPatch string `json:"browser_args_withpatch"`
	Description          string `json:"desc"`
	ChromiumPatch        string `json:"chromium_patch"`
	BlinkPatch           string `json:"blink_patch"`
	SkiaPatch            string `json:"skia_patch"`
	CatapultPatch        string `json:"catapult_patch"`
	BenchmarkPatch       string `json:"benchmark_patch"`
}

func (task *AddTaskVars) GetInsertQueryAndBinds() (string, []interface{}, error) {
	if task.Benchmark == "" ||
		task.Platform == "" ||
		task.PageSets == "" ||
		task.RepeatRuns == "" ||
		task.RunInParallel == "" ||
		task.Description == "" {
		return "", nil, fmt.Errorf("Invalid parameters")
	}
	customWebpages, err := ctfeutil.GetQualifiedCustomWebpages(task.CustomWebpages, task.BenchmarkArgs)
	if err != nil {
		return "", nil, err
	}
	if err := ctfeutil.CheckLengths([]ctfeutil.LengthCheck{
		{Name: "benchmark", Value: task.Benchmark, Limit: 100},
		{Name: "platform", Value: task.Platform, Limit: 100},
		{Name: "page_sets", Value: task.PageSets, Limit: 100},
		{Name: "benchmark_args", Value: task.BenchmarkArgs, Limit: 255},
		{Name: "browser_args_nopatch", Value: task.BrowserArgsNoPatch, Limit: 255},
		{Name: "browser_args_withpatch", Value: task.BrowserArgsWithPatch, Limit: 255},
		{Name: "desc", Value: task.Description, Limit: 255},
		{Name: "custom_webpages", Value: strings.Join(customWebpages, ","), Limit: db.LONG_TEXT_MAX_LENGTH},
		{Name: "chromium_patch", Value: task.ChromiumPatch, Limit: db.LONG_TEXT_MAX_LENGTH},
		{Name: "blink_patch", Value: task.BlinkPatch, Limit: db.LONG_TEXT_MAX_LENGTH},
		{Name: "skia_patch", Value: task.SkiaPatch, Limit: db.LONG_TEXT_MAX_LENGTH},
		{Name: "catapult_patch", Value: task.CatapultPatch, Limit: db.LONG_TEXT_MAX_LENGTH},
		{Name: "benchmark_patch", Value: task.BenchmarkPatch, Limit: db.LONG_TEXT_MAX_LENGTH},
	}); err != nil {
		return "", nil, err
	}
	runInParallel := 0
	if strings.EqualFold(task.RunInParallel, "True") {
		runInParallel = 1
	}
	return fmt.Sprintf("INSERT INTO %s (username,benchmark,platform,page_sets,custom_webpages,repeat_runs,run_in_parallel, benchmark_args,browser_args_nopatch,browser_args_withpatch,description,chromium_patch,blink_patch,skia_patch,catapult_patch,benchmark_patch,ts_added,repeat_after_days) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?);",
			db.TABLE_CHROMIUM_PERF_TASKS),
		[]interface{}{
			task.Username,
			task.Benchmark,
			task.Platform,
			task.PageSets,
			strings.Join(customWebpages, ","),
			task.RepeatRuns,
			runInParallel,
			task.BenchmarkArgs,
			task.BrowserArgsNoPatch,
			task.BrowserArgsWithPatch,
			task.Description,
			task.ChromiumPatch,
			task.BlinkPatch,
			task.SkiaPatch,
			task.CatapultPatch,
			task.BenchmarkPatch,
			task.TsAdded,
			task.RepeatAfterDays,
		},
		nil
}

func addTaskHandler(w http.ResponseWriter, r *http.Request) {
	task_common.AddTaskHandler(w, r, &AddTaskVars{})
}

func getTasksHandler(w http.ResponseWriter, r *http.Request) {
	task_common.GetTasksHandler(&DBTask{}, w, r)
}

type UpdateVars struct {
	task_common.UpdateTaskCommonVars

	Results            sql.NullString
	NoPatchRawOutput   sql.NullString
	WithPatchRawOutput sql.NullString
}

func (vars *UpdateVars) UriPath() string {
	return ctfeutil.UPDATE_CHROMIUM_PERF_TASK_POST_URI
}

func (task *UpdateVars) GetUpdateExtraClausesAndBinds() ([]string, []interface{}, error) {
	if err := ctfeutil.CheckLengths([]ctfeutil.LengthCheck{
		{Name: "NoPatchRawOutput", Value: task.NoPatchRawOutput.String, Limit: 255},
		{Name: "WithPatchRawOutput", Value: task.WithPatchRawOutput.String, Limit: 255},
		{Name: "Results", Value: task.Results.String, Limit: 255},
	}); err != nil {
		return nil, nil, err
	}
	clauses := []string{}
	args := []interface{}{}
	if task.Results.Valid {
		clauses = append(clauses, "results = ?")
		args = append(args, task.Results.String)
	}
	if task.NoPatchRawOutput.Valid {
		clauses = append(clauses, "nopatch_raw_output = ?")
		args = append(args, task.NoPatchRawOutput.String)
	}
	if task.WithPatchRawOutput.Valid {
		clauses = append(clauses, "withpatch_raw_output = ?")
		args = append(args, task.WithPatchRawOutput.String)
	}
	return clauses, args, nil
}

func updateTaskHandler(w http.ResponseWriter, r *http.Request) {
	task_common.UpdateTaskHandler(&UpdateVars{}, db.TABLE_CHROMIUM_PERF_TASKS, w, r)
}

func deleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	task_common.DeleteTaskHandler(&DBTask{}, w, r)
}

func redoTaskHandler(w http.ResponseWriter, r *http.Request) {
	task_common.RedoTaskHandler(&DBTask{}, w, r)
}

func runsHistoryView(w http.ResponseWriter, r *http.Request) {
	ctfeutil.ExecuteSimpleTemplate(runsHistoryTemplate, w, r)
}

func AddHandlers(r *mux.Router) {
	ctfeutil.AddForceLoginHandler(r, "/", "GET", addTaskView)
	ctfeutil.AddForceLoginHandler(r, "/"+ctfeutil.CHROMIUM_PERF_URI, "GET", addTaskView)
	ctfeutil.AddForceLoginHandler(r, "/"+ctfeutil.CHROMIUM_PERF_RUNS_URI, "GET", runsHistoryView)

	ctfeutil.AddForceLoginHandler(r, "/"+ctfeutil.ADD_CHROMIUM_PERF_TASK_POST_URI, "POST", addTaskHandler)
	ctfeutil.AddForceLoginHandler(r, "/"+ctfeutil.GET_CHROMIUM_PERF_TASKS_POST_URI, "POST", getTasksHandler)
	ctfeutil.AddForceLoginHandler(r, "/"+ctfeutil.DELETE_CHROMIUM_PERF_TASK_POST_URI, "POST", deleteTaskHandler)
	ctfeutil.AddForceLoginHandler(r, "/"+ctfeutil.REDO_CHROMIUM_PERF_TASK_POST_URI, "POST", redoTaskHandler)

	// Do not add force login handler for update methods. They use webhooks for authentication.
	r.HandleFunc("/"+ctfeutil.UPDATE_CHROMIUM_PERF_TASK_POST_URI, updateTaskHandler).Methods("POST")
}
