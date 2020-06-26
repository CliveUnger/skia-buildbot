/**
 * @fileoverview The bulk of the Analysis page of CT.
 */

import 'elements-sk/icon/delete-icon-sk';
import 'elements-sk/icon/cancel-icon-sk';
import 'elements-sk/icon/check-circle-icon-sk';
import 'elements-sk/icon/help-icon-sk';
import 'elements-sk/toast-sk';
import '../../../infra-sk/modules/confirm-dialog-sk';
import '../suggest-input-sk';
import '../input-sk';
import '../patch-sk';
import '../pageset-selector-sk';
import '../task-repeater-sk';
import '../task-priority-sk';

import { $$, $ } from 'common-sk/modules/dom';
import { define } from 'elements-sk/define';
import 'elements-sk/select-sk';
import { errorMessage } from 'elements-sk/errorMessage';
import { html } from 'lit-html';

import { ElementSk } from '../../../infra-sk/modules/ElementSk';
import {
  combineClDescriptions,
  missingLiveSitesWithCustomWebpages,
  moreThanThreeActiveTasksChecker,
  fetchBenchmarksAndPlatforms,
} from '../ctfe_utils';

// Chromium analysis doesn't support 1M pageset, and only Linux supports 100k.
const unsupportedPageSetStrings = ['All', '100k'];
const unsupportedPageSetStringsLinux = ['All'];

const template = (el) => html`
<confirm-dialog-sk id=confirm_dialog></confirm-dialog-sk>

<table class=options>
  <tr>
    <td>Benchmark Name</td>
    <td>
      <suggest-input-sk
        id=benchmark_name
        .options=${el._benchmarks}
        .label=${'Hit <enter> at end if entering custom benchmark'}
        accept-custom-value
        @value-changed=${el._refreshBenchmarkDoc}
      ></suggest-input-sk>
      <div>
        <a hidden id=benchmark_doc href=#
        target=_blank rel="noopener noreferrer">
          Documentation
        </a>
      </div>
    </td>
  </tr>
  <tr>
    <td>Target Platform</td>
    <td>
      <select-sk id=platform_selector @selection-changed=${el._platformChanged}>
        ${el._platforms.map((p, i) => (html`<div ?selected=${i === 1}>${p[1]}</div>`))}
      </select-sk>
    </td>
  </tr>
  <tr>
    <td>
      Run on GCE
    </td>
    <td>
      <select-sk id=run_on_gce>
        <div selected id=gce_true>True</div>
        <div id=gce_false>False</div>
      </select-sk>
    </td>
  </tr>
  <tr>
    <td>PageSets Type</td>
    <td>
      <pageset-selector-sk id=pageset_selector></pageset-selector-sk>
    </td>
  </tr>
  <tr>
    <td>
      Run in Parallel<br/>
      Read about the trade-offs <a href="https://docs.google.com/document/d/1GhqosQcwsy6F-eBAmFn_ITDF7_Iv_rY9FhCKwAnk9qQ/edit?pli=1#heading=h.xz46aihphb8z">here</a>
    </td>
    <td>
      <select-sk id=run_in_parallel @selection-changed=${el._updatePageSets}>
        <div selected>True</div>
        <div>False</div>
      </select-sk>
    </td>
  </tr>
  <tr>
    <td>Look for text in stdout</td>
    <td>
      <input-sk value="" id=match_stdout_txt class=long-field></input-sk>
      <span class=smaller-font><b>Note:</b> All lines that contain this field in stdout will show up under CT_stdout_lines in the output CSV.</span><br/>
      <span class=smaller-font><b>Note:</b> The count of non-overlapping exact matches of this field in stdout will show up under CT_stdout_count in the output CSV.</span><br/>
    </td>
  </tr>
  <tr>
    <td>Benchmark Arguments</td>
    <td>
      <input-sk value="--output-format=csv --skip-typ-expectations-tags-validation --legacy-json-trace-format" id=benchmark_args class=long-field></input-sk>
      <span class=smaller-font><b>Note:</b> Use --num-analysis-retries=[num] to specify how many times run_benchmark should be retried. 2 is the default. 0 calls run_benchmark once.</span><br/>
      <span class=smaller-font><b>Note:</b> Use --run-benchmark-timeout=[secs] to specify the timeout of the run_benchmark script. 300 is the default.</span><br/>
      <span class=smaller-font><b>Note:</b> Use --max-pages-per-bot=[num] to specify the number of pages to run per bot. 100 is the default.</span>
    </td>
  </tr>
  <tr>
    <td>Browser Arguments</td>
    <td>
      <input-sk value="" id=browser_args class=long-field></input-sk>
    </td>
  </tr>
  <tr>
    <td>Field Value Column Name</td>
    <td>
      <input-sk value="avg" id=value_column_name class="medium-field"></input-sk>
      <span class=smaller-font>Which column's entries to use as field values.</span>
    </td>
  </tr>
  <tr>
    <td>
      Chromium Git patch (optional)<br/>
      Applied to Chromium ToT<br/>
      or to the hash specified below.
    </td>
    <td>
      <patch-sk id=chromium_patch
                patchType=chromium
                @cl-description-changed=${el._patchChanged}>
      </patch-sk>
    </td>
  </tr>
  <tr>
    <td>
      Custom APK location (optional)<br/> (See
      <a href="https://bugs.chromium.org/p/skia/issues/detail?id=9805">skbug/9805</a>)
    </td>
    <td>
      <input-sk value="" id=apk_gs_path label="Eg: gs://chrome-unsigned/android-B0urB0N/73.0.3655.0/arm_64/ChromeModern.apk" class=long-field></input-sk>
    </td>
  </tr>
  <tr>
    <td>
      Telemetry Isolate Hash (optional))<br/> (See
      <a href="https://bugs.chromium.org/p/skia/issues/detail?id=9853">skbug/9853</a>)
    </td>
    <td>
      <input-sk value="" id=telemetry_isolate_hash class=long-field></input-sk>
    </td>
  </tr>
  <tr>
    <td>Chromium hash to sync to (optional)<br/></td>
    <td>
      <input-sk value="" id=chromium_hash class=long-field></input-sk>
    </td>
  </tr>
  <tr>
    <td>
      Skia Git patch (optional)<br/>
      Applied to Skia Rev in <a href="https://chromium.googlesource.com/chromium/src/+show/HEAD/DEPS">DEPS</a>
    </td>
    <td>
      <patch-sk id=skia_patch
                patchType=skia
                @cl-description-changed=${el._patchChanged}>
      </patch-sk>
    </td>
  </tr>
  <tr>
    <td>
      V8 Git patch (optional)<br/>
      Applied to V8 Rev in <a href="https://chromium.googlesource.com/chromium/src/+show/HEAD/DEPS">DEPS</a>
    </td>
    <td>
      <patch-sk id=v8_patch
                patchType=v8
                @cl-description-changed=${el._patchChanged}>
      </patch-sk>
    </td>
  </tr>
  <tr>
    <td>
      Catapult Git patch (optional)<br/>
      Applied to Catapult Rev in <a href="https://chromium.googlesource.com/chromium/src/+show/HEAD/DEPS">DEPS</a>
    </td>
    <td>
      <patch-sk id=catapult_patch
                patchType=catapult
                @cl-description-changed=${el._patchChanged}>
      </patch-sk>
    </td>
  </tr>
  <tr>
    <td>Repeat this task</td>
    <td>
      <task-repeater-sk id=repeat_after_days></task-repeater-sk>
    </td>
  </tr>
  <tr>
    <td>Task Priority</td>
    <td>
      <task-priority-sk id=task_priority></task-priority-sk>
    </td>
  </tr>
  <tr>
    <td>
      Notifications CC list (optional)<br/>
      Email will be sent by ct@skia.org
    </td>
    <td>
      <input-sk value="" id=cc_list label="email1,email2,email3" class=long-field></input-sk>
    </td>
  </tr>
  <tr>
    <td>
      Group name (optional)<br/>
      Will be used to track runs
    </td>
    <td>
      <input-sk value="" id=group_name class=long-field></input-sk>
    </td>
  </tr>
  <tr>
    <td>Description</td>
    <td>
      <input-sk value="" id=description label="Description is required" class=long-field></input-sk>
    </td>
  </tr>
  <tr>
    <td colspan="2" class="center">
      <div class="triggering-spinner">
        <spinner-sk .active=${el._triggeringTask} alt="Trigger task"></spinner-sk>
      </div>
      <button id=submit ?disabled=${el._triggeringTask} @click=${el._validateTask}>Queue Task</button>
    </td>
  </tr>
  <tr>
    <td colspan=2 class=center>
      <button id=view_history @click=${el._gotoRunsHistory}>View runs history</button>
    </td>
  </tr>
</table>
`;

define('chromium-analysis-sk', class extends ElementSk {
  constructor() {
    super(template);
    this._benchmarksToDocs = {};
    this._benchmarks = [];
    this._platforms = [];
    this._triggeringTask = false;
    this._unsupportedPageSets = unsupportedPageSetStringsLinux;
    this._moreThanThreeActiveTasks = moreThanThreeActiveTasksChecker();
  }

  connectedCallback() {
    super.connectedCallback();
    this._render();
    fetchBenchmarksAndPlatforms((json) => {
      this._benchmarksToDocs = json.benchmarks;
      this._benchmarks = Object.keys(json.benchmarks);
      // { 'p1' : 'p1Desc', ... } -> [[p1, p1Desc], ...]
      // Allows rendering descriptions in the select-sk, and converting the
      // integer selection to platform name easily.
      this._platforms = Object.entries(json.platforms);
      this._render();
      // Do this after the template is rendered, or else it fails, and don't
      // inline a child 'selected' attribute since it won't rationalize in
      // select-sk until later via the mutationObserver.
      $$('#platform_selector', this).selection = 1;
      // This gets the defaults in a valid state.
      this._platformChanged();
    });
  }

  _refreshBenchmarkDoc(e) {
    const benchmarkName = e.detail.value;
    const docElement = $$('#benchmark_doc', this);
    if (benchmarkName && this._benchmarksToDocs[benchmarkName]) {
      docElement.hidden = false;
      docElement.href = this._benchmarksToDocs[benchmarkName];
    } else {
      docElement.hidden = true;
      docElement.href = '#';
    }
  }

  _platformChanged() {
    const trueIndex = 0;
    const falseIndex = 1;
    const platform = this._platform();
    let offerGCETrue = true;
    let offerGCEFalse = true;
    let offerParallelTrue = true;
    if (platform === 'Android') {
      offerGCETrue = false;
      offerParallelTrue = false;
    } else if (platform === 'Windows') {
      offerGCEFalse = false;
    }
    // We default to use GCE for Linux, require if for Windows, and
    // disallow it for Android.
    const runOnGCE = $$('#run_on_gce', this);
    runOnGCE.children[trueIndex].hidden = !offerGCETrue;
    runOnGCE.children[falseIndex].hidden = !offerGCEFalse;
    runOnGCE.selection = offerGCETrue ? trueIndex : falseIndex;

    // We default to run in parallel, except for Android which disallows it.
    const runInParallel = $$('#run_in_parallel', this);
    runInParallel.children[trueIndex].hidden = !offerParallelTrue;
    runInParallel.selection = offerParallelTrue ? trueIndex : falseIndex;

    this._updatePageSets();
  }

  _updatePageSets() {
    const platform = this._platform();
    const runInParallel = this._runInParallel();
    const unsupportedPageSets = (platform === 'Linux' && runInParallel)
      ? unsupportedPageSetStringsLinux
      : unsupportedPageSetStrings;
    const pageSetDefault = (platform === 'Android')
      ? 'Mobile10k'
      : '10k';
    const pagesetSelector = $$('pageset-selector-sk', this);
    pagesetSelector.hideIfKeyContains = unsupportedPageSets;
    pagesetSelector.selected = pageSetDefault;
  }

  _platform() {
    return this._platforms[$$('#platform_selector', this).selection][0];
  }

  _runInParallel() {
    return $$('#run_in_parallel', this).selection === 0;
  }

  _patchChanged() {
    $$('#description', this).value = combineClDescriptions(
      $('patch-sk', this).map((patch) => patch.clDescription),
    );
  }

  _validateTask() {
    if (!$('patch-sk', this).every((patch) => patch.validate())) {
      return;
    }
    if (!$$('#description', this).value) {
      errorMessage('Please specify a description');
      $$('#description', this).focus();
      return;
    }
    if (!$$('#benchmark_name', this).value) {
      errorMessage('Please specify a benchmark');
      $$('#benchmark_name', this).focus();
      return;
    }
    if (missingLiveSitesWithCustomWebpages(
      $$('#pageset_selector', this).customPages, $$('#benchmark_args', this).value,
    )) {
      $$('#benchmark_args', this).focus();
      return;
    }
    if (this._moreThanThreeActiveTasks()) {
      return;
    }
    $$('#confirm_dialog', this).open('Proceed with queueing task?')
      .then(() => this._queueTask())
      .catch(() => {
        errorMessage('Unable to queue task');
      });
  }

  _queueTask() {
    this._triggeringTask = true;
    const params = {};
    params.benchmark = $$('#benchmark_name', this).value;
    params.platform = this._platforms[$$('#platform_selector', this).selection][0];
    params.page_sets = $$('#pageset_selector', this).selected;
    params.run_on_gce = $$('#run_on_gce', this).selection === 0;
    params.match_stdout_txt = $$('#match_stdout_txt', this).value;
    params.apk_gs_path = $$('#apk_gs_path', this).value;
    params.telemetry_isolate_hash = $$('#telemetry_isolate_hash', this).value;
    params.custom_webpages = $$('#pageset_selector', this).customPages;
    params.run_in_parallel = $$('#run_in_parallel', this).selection === 0;
    params.benchmark_args = $$('#benchmark_args', this).value;
    params.browser_args = $$('#browser_args', this).value;
    params.value_column_name = $$('#value_column_name', this).value;
    params.desc = $$('#description', this).value;
    params.chromium_patch = $$('#chromium_patch', this).patch;
    params.skia_patch = $$('#skia_patch', this).patch;
    params.v8_patch = $$('#v8_patch', this).patch;
    params.catapult_patch = $$('#catapult_patch', this).patch;
    params.chromium_hash = $$('#chromium_hash', this).value;
    params.repeat_after_days = $$('#repeat_after_days', this).frequency;
    params.task_priority = $$('#task_priority', this).priority;
    if ($$('#cc_list', this).value) {
      params.cc_list = $$('#cc_list', this).value.split(',');
    }
    if ($$('#group_name', this).value) {
      params.group_name = $$('#group_name', this).value;
    }

    fetch('/_/add_chromium_analysis_task', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })
      .then(() => this._gotoRunsHistory())
      .catch((e) => {
        this._triggeringTask = false;
        errorMessage(e);
      });
  }

  _gotoRunsHistory() {
    window.location.href = '/chromium_analysis_runs/';
  }
});
