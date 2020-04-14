import './index';
import { $$ } from 'common-sk/modules/dom';
import { fetchMock } from 'fetch-mock';
import { delay, isPuppeteerTest } from '../demo_util';
import { trstatus } from './test_data';

const fakeRpcDelayMillis = isPuppeteerTest() ? 5 : 300;

fetchMock.get('/json/trstatus', () => {
  if ($$('#simulate-rpc-failure').checked) {
    return 500; // Fake an internal server error.
  }

  if (!isPuppeteerTest()) {
    // Increase negative triaged count by 1 at every update cycle. Skip this for puppeteer tests
    // to avoid non-determinism (e.g. with order of tests).
    trstatus.corpStatus.forEach((corpus) => corpus.negativeCount++);
  }

  return delay(trstatus, fakeRpcDelayMillis);
});

// Create the components after we've had a chance to mock the JSON endpoint.

const handleCorpusSelected = (e) => {
  const corpus = e.detail.corpus;
  const log = $$('#event-log');
  log.value = `${corpus.padEnd(15) + new Date()}\n${log.value}`;
};

// Default corpus renderer function.
const el1 = document.createElement('corpus-selector-sk');
el1.selectedCorpus = 'gm';
el1.addEventListener('corpus-selected', handleCorpusSelected);
$$('#default').appendChild(el1);

// Custom corpus renderer function.
const el2 = document.createElement('corpus-selector-sk');
el2.selectedCorpus = 'gm';
if (!isPuppeteerTest()) {
  el2.setAttribute('update-freq-seconds', '3');
}
el2.corpusRendererFn = (corpus) => `${corpus.name} : ${corpus.untriagedCount} / ${corpus.negativeCount}`;
el2.addEventListener('corpus-selected', handleCorpusSelected);
$$('#custom-fn').appendChild(el2);

// Custom corpus renderer function (long).
const el3 = document.createElement('corpus-selector-sk');
el3.selectedCorpus = 'gm';
el3.corpusRendererFn = (corpus) => `${corpus.name} : yadda yadda yadda yadda yadda`;
el3.addEventListener('corpus-selected', handleCorpusSelected);
$$('#custom-fn-long-corpus').appendChild(el3);
